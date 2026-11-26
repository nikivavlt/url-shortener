//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	dockercontainer "github.com/moby/moby/api/types/container"
	mobynetwork "github.com/moby/moby/api/types/network"
	"github.com/segmentio/kafka-go"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	containerStartTimeout = 2 * time.Minute
	serviceStartTimeout   = 45 * time.Second
)

var testSystem *system

type system struct {
	postgres   tc.Container
	mongo      tc.Container
	redis      tc.Container
	kafka      tc.Container
	clickhouse tc.Container

	kafkaBroker string
	gatewayURL  string
	processes   []*serviceProcess
}

func TestMain(m *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	var err error
	testSystem, err = startSystem(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "start integration environment:", err)
		os.Exit(1)
	}

	code := m.Run()
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cleanupCancel()
	if err := testSystem.close(cleanupCtx); err != nil {
		fmt.Fprintln(os.Stderr, "clean integration environment:", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

// NOTE: tests run in source order. TestRegisterLoginCreateShortURLRedirectPublishesClick
// reads the FIRST message of click_events, so it must stay above every test that clicks.

func TestRegisterLoginCreateShortURLRedirectPublishesClick(t *testing.T) {
	email := uniqueEmail(t)
	password := "strong-password"
	userID := register(t, email, password)
	token := login(t, email, password)

	code := createShortURL(t, token, "https://example.com/landing")

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     []string{testSystem.kafkaBroker},
		Topic:       "click_events",
		StartOffset: kafka.FirstOffset,
		MinBytes:    1,
		MaxBytes:    1 << 20,
		MaxWait:     time.Second,
	})
	t.Cleanup(func() { _ = reader.Close() })

	request, err := http.NewRequest(http.MethodGet, testSystem.gatewayURL+"/"+code, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.2")
	request.Header.Set("User-Agent", "url-shortener-integration")
	request.Header.Set("Referer", "https://referrer.example/page")

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusFound {
		t.Fatalf("redirect status = %d, want %d", response.StatusCode, http.StatusFound)
	}
	if got := response.Header.Get("Location"); got != "https://example.com/landing" {
		t.Fatalf("redirect location = %q", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	message, err := reader.FetchMessage(ctx)
	if err != nil {
		t.Fatalf("read click_events message: %v", err)
	}

	var event struct {
		ShortCode string    `json:"short_code"`
		UserID    string    `json:"user_id"`
		ClickedAt time.Time `json:"clicked_at"`
		IP        string    `json:"ip"`
		UserAgent string    `json:"user_agent"`
		Referer   string    `json:"referer"`
	}
	if err := json.Unmarshal(message.Value, &event); err != nil {
		t.Fatalf("decode click event: %v; payload=%s", err, message.Value)
	}
	if event.ShortCode != code || event.UserID != userID || event.IP != "203.0.113.7" ||
		event.UserAgent != "url-shortener-integration" || event.Referer != "https://referrer.example/page" {
		t.Fatalf("unexpected click event: %+v", event)
	}
	if event.ClickedAt.IsZero() {
		t.Fatal("click event has no clicked_at timestamp")
	}
}

func TestRegisteredUserCanListCreatedURL(t *testing.T) {
	email := uniqueEmail(t)
	password := "strong-password"
	register(t, email, password)
	token := login(t, email, password)
	code := createShortURL(t, token, "https://example.com/listed")

	request, err := http.NewRequest(http.MethodGet, testSystem.gatewayURL+"/api/v1/urls", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response := do(t, request, http.StatusOK)
	defer response.Body.Close()

	var body struct {
		URLs []struct {
			ShortCode   string `json:"short_code"`
			OriginalURL string `json:"original_url"`
		} `json:"urls"`
	}
	decodeJSON(t, response.Body, &body)
	if len(body.URLs) != 1 || body.URLs[0].ShortCode != code || body.URLs[0].OriginalURL != "https://example.com/listed" {
		t.Fatalf("listed URLs = %+v", body.URLs)
	}
}

func TestRedirectWorksOnCacheHit(t *testing.T) {
	email := uniqueEmail(t)
	password := "strong-password"

	register(t, email, password)
	token := login(t, email, password)
	code := createShortURL(t, token, "https://example.com/cache-test")

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	for i := 1; i <= 2; i++ {
		request, err := http.NewRequest(
			http.MethodGet,
			testSystem.gatewayURL+"/"+code,
			nil,
		)
		if err != nil {
			t.Fatal(err)
		}

		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}

		if response.StatusCode != http.StatusFound {
			body, _ := io.ReadAll(response.Body)
			response.Body.Close()
			t.Fatalf(
				"request %d: status = %d, want %d; body=%s",
				i, response.StatusCode, http.StatusFound, body,
			)
		}

		if got := response.Header.Get("Location"); got != "https://example.com/cache-test" {
			response.Body.Close()
			t.Fatalf("request %d: location = %q", i, got)
		}

		response.Body.Close()
	}
}

func TestRedirectUnknownShortCodeReturnsNotFound(t *testing.T) {
	request, err := http.NewRequest(
		http.MethodGet,
		testSystem.gatewayURL+"/unknown-code",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	response := do(t, request, http.StatusNotFound)
	response.Body.Close()
}

func TestListURLsRequiresAuthentication(t *testing.T) {
	request, err := http.NewRequest(
		http.MethodGet,
		testSystem.gatewayURL+"/api/v1/urls",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	response := do(t, request, http.StatusUnauthorized)
	response.Body.Close()
}

// ---------------------------------------------------------------------------
// Analytics: gateway -> url service -> Kafka -> analytics consumer -> ClickHouse -> gateway
// ---------------------------------------------------------------------------

type analyticsBody struct {
	TotalClicks int64 `json:"total_clicks"`
	ClicksByDay []struct {
		Date   string `json:"date"`
		Clicks int64  `json:"clicks"`
	} `json:"clicks_by_day"`
	TopReferers []struct {
		Referer string `json:"referer"`
		Clicks  int64  `json:"clicks"`
	} `json:"top_referers"`
}

func TestAnalyticsAggregatesClicks(t *testing.T) {
	email := uniqueEmail(t)
	register(t, email, "strong-password")
	token := login(t, email, "strong-password")
	code := createShortURL(t, token, "https://example.com/analytics")

	for i := 0; i < 3; i++ {
		click(t, code, "https://a.example/")
	}
	click(t, code, "https://b.example/")
	click(t, code, "")

	// The consumer flushes every 5s or every 100 events, so poll.
	got := waitForTotalClicks(t, token, code, 5)

	var byDay int64
	for _, d := range got.ClicksByDay {
		byDay += d.Clicks // summed so a midnight rollover can't make the test flaky
	}
	if byDay != 5 {
		t.Fatalf("clicks by day sum = %d, want 5 (%+v)", byDay, got.ClicksByDay)
	}
	if len(got.TopReferers) == 0 || got.TopReferers[0].Referer != "https://a.example/" || got.TopReferers[0].Clicks != 3 {
		t.Fatalf("top referers = %+v, want a.example first with 3", got.TopReferers)
	}
}

func TestAnalyticsForbiddenForNonOwner(t *testing.T) {
	ownerEmail := uniqueEmail(t) + ".owner"
	register(t, ownerEmail, "strong-password")
	ownerToken := login(t, ownerEmail, "strong-password")
	code := createShortURL(t, ownerToken, "https://example.com/private")

	otherEmail := uniqueEmail(t) + ".other"
	register(t, otherEmail, "strong-password")
	otherToken := login(t, otherEmail, "strong-password")

	response := do(t, authedGet(t, "/api/v1/analytics/"+code, otherToken), http.StatusForbidden)
	_ = response.Body.Close()
}

func TestAnalyticsUnknownCodeIsNotFound(t *testing.T) {
	email := uniqueEmail(t)
	register(t, email, "strong-password")
	token := login(t, email, "strong-password")

	response := do(t, authedGet(t, "/api/v1/analytics/does-not-exist", token), http.StatusNotFound)
	_ = response.Body.Close()
}

func TestAnalyticsRequiresAuth(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, testSystem.gatewayURL+"/api/v1/analytics/anything", nil)
	if err != nil {
		t.Fatal(err)
	}
	response := do(t, request, http.StatusUnauthorized)
	_ = response.Body.Close()
}

func TestAnalyticsInvalidDays(t *testing.T) {
	email := uniqueEmail(t)
	register(t, email, "strong-password")
	token := login(t, email, "strong-password")
	code := createShortURL(t, token, "https://example.com/days")

	response := do(t, authedGet(t, "/api/v1/analytics/"+code+"?days=abc", token), http.StatusBadRequest)
	_ = response.Body.Close()
}

func authedGet(t *testing.T, path, token string) *http.Request {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, testSystem.gatewayURL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	return request
}

// click performs one redirect without following it.
func click(t *testing.T, code, referer string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, testSystem.gatewayURL+"/"+code, nil)
	if err != nil {
		t.Fatal(err)
	}
	if referer != "" {
		request.Header.Set("Referer", referer)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusFound {
		t.Fatalf("redirect status = %d, want %d", response.StatusCode, http.StatusFound)
	}
}

func waitForTotalClicks(t *testing.T, token, code string, want int64) analyticsBody {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var last analyticsBody
	for time.Now().Before(deadline) {
		response := do(t, authedGet(t, "/api/v1/analytics/"+code, token), http.StatusOK)
		last = analyticsBody{}
		decodeJSON(t, response.Body, &last)
		_ = response.Body.Close()
		if last.TotalClicks == want {
			return last
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("total_clicks = %d after 30s, want %d", last.TotalClicks, want)
	return last
}

// ---------------------------------------------------------------------------
// Harness
// ---------------------------------------------------------------------------

func startSystem(ctx context.Context) (*system, error) {
	s := &system{}
	var err error

	if s.postgres, err = startContainer(ctx, tc.ContainerRequest{
		Image:        "postgres:16-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_DB":       "url_shortener",
			"POSTGRES_USER":     "postgres",
			"POSTGRES_PASSWORD": "postgres",
		},
		WaitingFor: wait.ForListeningPort("5432/tcp").WithStartupTimeout(containerStartTimeout),
	}); err != nil {
		return nil, fmt.Errorf("postgres: %w", err)
	}
	if s.mongo, err = startContainer(ctx, tc.ContainerRequest{
		Image:        "mongo:7",
		ExposedPorts: []string{"27017/tcp"},
		WaitingFor:   wait.ForListeningPort("27017/tcp").WithStartupTimeout(containerStartTimeout),
	}); err != nil {
		_ = s.close(context.Background())
		return nil, fmt.Errorf("mongodb: %w", err)
	}
	if s.redis, err = startContainer(ctx, tc.ContainerRequest{
		Image:        "redis:7-alpine",
		ExposedPorts: []string{"6379/tcp"},
		WaitingFor:   wait.ForListeningPort("6379/tcp").WithStartupTimeout(containerStartTimeout),
	}); err != nil {
		_ = s.close(context.Background())
		return nil, fmt.Errorf("redis: %w", err)
	}

	// The HTTP /ping wait is more reliable than waiting on port 9000: the native
	// port opens before ClickHouse finishes initializing.
	if s.clickhouse, err = startContainer(ctx, tc.ContainerRequest{
		Image:        "clickhouse/clickhouse-server:24.3-alpine",
		ExposedPorts: []string{"9000/tcp", "8123/tcp"},
		Env: map[string]string{
			"CLICKHOUSE_DB":       "analytics",
			"CLICKHOUSE_USER":     "test",
			"CLICKHOUSE_PASSWORD": "test",
		},
		WaitingFor: wait.ForHTTP("/ping").WithPort("8123/tcp").WithStartupTimeout(containerStartTimeout),
	}); err != nil {
		_ = s.close(context.Background())
		return nil, fmt.Errorf("clickhouse: %w", err)
	}

	kafkaPort, err := freePort()
	if err != nil {
		_ = s.close(context.Background())
		return nil, fmt.Errorf("reserve kafka port: %w", err)
	}
	s.kafka, err = startContainer(ctx, kafkaContainerRequest(kafkaPort))
	if err != nil {
		_ = s.close(context.Background())
		return nil, fmt.Errorf("kafka: %w", err)
	}
	s.kafkaBroker = net.JoinHostPort("127.0.0.1", strconv.Itoa(kafkaPort))
	if err := createClickEventsTopic(ctx, s.kafkaBroker); err != nil {
		_ = s.close(context.Background())
		return nil, err
	}

	if err := s.startServices(ctx); err != nil {
		_ = s.close(context.Background())
		return nil, err
	}
	return s, nil
}

func startContainer(ctx context.Context, request tc.ContainerRequest) (tc.Container, error) {
	return tc.GenericContainer(ctx, tc.GenericContainerRequest{ContainerRequest: request, Started: true})
}

func kafkaContainerRequest(hostPort int) tc.ContainerRequest {
	port := nat.Port("9092/tcp")
	return tc.ContainerRequest{
		Image:        "apache/kafka:3.8.0",
		ExposedPorts: []string{string(port)},
		Env: map[string]string{
			"KAFKA_NODE_ID":                                  "1",
			"KAFKA_PROCESS_ROLES":                            "broker,controller",
			"KAFKA_LISTENERS":                                "PLAINTEXT://:9092,CONTROLLER://:9093",
			"KAFKA_ADVERTISED_LISTENERS":                     "PLAINTEXT://127.0.0.1:" + strconv.Itoa(hostPort),
			"KAFKA_CONTROLLER_QUORUM_VOTERS":                 "1@localhost:9093",
			"KAFKA_CONTROLLER_LISTENER_NAMES":                "CONTROLLER",
			"KAFKA_LISTENER_SECURITY_PROTOCOL_MAP":           "CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT",
			"KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR":         "1",
			"KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR": "1",
			"KAFKA_TRANSACTION_STATE_LOG_MIN_ISR":            "1",
		},
		HostConfigModifier: func(hostConfig *dockercontainer.HostConfig) {
			hostConfig.PortBindings = mobynetwork.PortMap{
				mobynetwork.MustParsePort("9092/tcp"): []mobynetwork.PortBinding{
					{
						HostIP:   netip.MustParseAddr("127.0.0.1"),
						HostPort: strconv.Itoa(hostPort),
					},
				},
			}
		},
		WaitingFor: wait.ForListeningPort(string(port)).WithStartupTimeout(containerStartTimeout),
	}
}

func createClickEventsTopic(ctx context.Context, broker string) error {
	deadline := time.Now().Add(containerStartTimeout)
	for time.Now().Before(deadline) {
		conn, err := kafka.Dial("tcp", broker)
		if err == nil {
			err = conn.CreateTopics(kafka.TopicConfig{Topic: "click_events", NumPartitions: 1, ReplicationFactor: 1})
			closeErr := conn.Close()
			if err == nil || strings.Contains(err.Error(), "TopicExists") || strings.Contains(err.Error(), "already exists") {
				return closeErr
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("create click_events topic: %w", ctx.Err())
		case <-time.After(time.Second):
		}
	}
	return errors.New("create click_events topic: kafka did not become ready")
}

func (s *system) startServices(ctx context.Context) error {
	postgresHost, postgresPort, err := endpoint(ctx, s.postgres, "5432/tcp")
	if err != nil {
		return err
	}
	mongoHost, mongoPort, err := endpoint(ctx, s.mongo, "27017/tcp")
	if err != nil {
		return err
	}
	redisHost, redisPort, err := endpoint(ctx, s.redis, "6379/tcp")
	if err != nil {
		return err
	}
	clickhouseHost, clickhousePort, err := endpoint(ctx, s.clickhouse, "9000/tcp")
	if err != nil {
		return err
	}

	userPort, err := freePort()
	if err != nil {
		return err
	}
	urlPort, err := freePort()
	if err != nil {
		return err
	}
	analyticsPort, err := freePort()
	if err != nil {
		return err
	}
	gatewayPort, err := freePort()
	if err != nil {
		return err
	}

	root, err := filepath.Abs("..")
	if err != nil {
		return err
	}
	binaries, err := buildServices(ctx, root)
	if err != nil {
		return err
	}

	userAddress := net.JoinHostPort("127.0.0.1", strconv.Itoa(userPort))
	urlAddress := net.JoinHostPort("127.0.0.1", strconv.Itoa(urlPort))
	analyticsAddress := net.JoinHostPort("127.0.0.1", strconv.Itoa(analyticsPort))
	s.gatewayURL = "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(gatewayPort))
	common := map[string]string{
		"REDIS_HOST": redisHost,
		"REDIS_PORT": redisPort,
	}

	userEnv := mergeEnv(common, map[string]string{
		"DATABASE_URL":      fmt.Sprintf("postgres://postgres:postgres@%s:%s/url_shortener?sslmode=disable", postgresHost, postgresPort),
		"JWT_SECRET":        "integration-test-secret",
		"ACCESS_TOKEN_TTL":  "1h",
		"REFRESH_TOKEN_TTL": "1h",
		"USER_SERVICE_PORT": strconv.Itoa(userPort),
	})
	if err := s.startService(root, binaries["user"], userEnv, userAddress); err != nil {
		return err
	}

	urlEnv := mergeEnv(common, map[string]string{
		"MONGO_URI":         "mongodb://" + net.JoinHostPort(mongoHost, mongoPort),
		"MONGO_DB":          "url_shortener",
		"USER_SERVICE_ADDR": userAddress,
		"URL_SERVICE_PORT":  strconv.Itoa(urlPort),
		"SHORT_URL_BASE":    s.gatewayURL,
		"KAFKA_BROKERS":     s.kafkaBroker,
	})
	if err := s.startService(root, binaries["url"], urlEnv, urlAddress); err != nil {
		return err
	}

	// Values set here win over analytics/.env: godotenv never overrides variables that are already set.
	analyticsEnv := map[string]string{
		"KAFKA_BROKERS":          s.kafkaBroker,
		"CLICKHOUSE_ADDR":        net.JoinHostPort(clickhouseHost, clickhousePort),
		"CLICKHOUSE_DB":          "analytics",
		"CLICKHOUSE_USERNAME":    "test",
		"CLICKHOUSE_PASSWORD":    "test",
		"URL_SERVICE_ADDR":       urlAddress,
		"ANALYTICS_SERVICE_PORT": strconv.Itoa(analyticsPort),
	}
	if err := s.startService(root, binaries["analytics"], analyticsEnv, analyticsAddress); err != nil {
		return err
	}

	gatewayEnv := map[string]string{
		"GATEWAY_PORT":           strconv.Itoa(gatewayPort),
		"USER_SERVICE_ADDR":      userAddress,
		"URL_SERVICE_ADDR":       urlAddress,
		"ANALYTICS_SERVICE_ADDR": analyticsAddress, // verify this name against gateway/internal/config
	}
	if err := s.startService(root, binaries["gateway"], gatewayEnv, net.JoinHostPort("127.0.0.1", strconv.Itoa(gatewayPort))); err != nil {
		return err
	}
	return nil
}

func buildServices(ctx context.Context, root string) (map[string]string, error) {
	binaries := make(map[string]string, 4)
	dir, err := os.MkdirTemp("", "url-shortener-integration-")
	if err != nil {
		return nil, err
	}
	for name, pkg := range map[string]string{
		"user":      "./user/cmd",
		"url":       "./url/cmd",
		"analytics": "./analytics/cmd",
		"gateway":   "./gateway/cmd",
	} {
		binary := filepath.Join(dir, name)
		command := exec.CommandContext(ctx, "go", "build", "-o", binary, pkg)
		command.Dir = root
		output, buildErr := command.CombinedOutput()
		if buildErr != nil {
			return nil, fmt.Errorf("build %s: %w\n%s", name, buildErr, output)
		}
		binaries[name] = binary
	}
	return binaries, nil
}

func endpoint(ctx context.Context, container tc.Container, port nat.Port) (string, string, error) {
	host, err := container.Host(ctx)
	if err != nil {
		return "", "", err
	}
	mapped, err := container.MappedPort(ctx, string(port))
	if err != nil {
		return "", "", err
	}
	return host, mapped.Port(), nil
}

func (s *system) startService(root, binary string, environment map[string]string, address string) error {
	process := &serviceProcess{cmd: exec.Command(binary)}
	process.cmd.Dir = root
	process.cmd.Env = envList(mergeEnv(nil, environment))
	process.cmd.Stdout = &process.output
	process.cmd.Stderr = &process.output
	if err := process.cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", filepath.Base(binary), err)
	}
	s.processes = append(s.processes, process)
	if err := waitForTCP(address, serviceStartTimeout); err != nil {
		return fmt.Errorf("%s did not start: %w\n%s", filepath.Base(binary), err, process.output.String())
	}
	return nil
}

type serviceProcess struct {
	cmd    *exec.Cmd
	output lockedBuffer
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(data)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func (s *system) close(ctx context.Context) error {
	var errs []error
	for i := len(s.processes) - 1; i >= 0; i-- {
		if err := s.processes[i].stop(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	for _, container := range []tc.Container{s.clickhouse, s.kafka, s.redis, s.mongo, s.postgres} {
		if container != nil {
			if err := container.Terminate(ctx); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func (p *serviceProcess) stop(ctx context.Context) error {
	if p.cmd.Process == nil {
		return nil
	}
	_ = p.cmd.Process.Signal(os.Interrupt)
	done := make(chan error, 1)
	go func() { done <- p.cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("%s: %w\n%s", filepath.Base(p.cmd.Path), err, p.output.String())
		}
		return nil
	case <-ctx.Done():
		_ = p.cmd.Process.Kill()
		return fmt.Errorf("stop %s: %w", filepath.Base(p.cmd.Path), ctx.Err())
	}
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func waitForTCP(address string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp", address, 250*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s", address)
}

func mergeEnv(base, overrides map[string]string) map[string]string {
	values := make(map[string]string)
	if base == nil {
		for _, item := range os.Environ() {
			key, value, ok := strings.Cut(item, "=")
			if ok {
				values[key] = value
			}
		}
	} else {
		for key, value := range base {
			values[key] = value
		}
	}
	for key, value := range overrides {
		values[key] = value
	}
	return values
}

func envList(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	items := make([]string, 0, len(keys))
	for _, key := range keys {
		items = append(items, key+"="+values[key])
	}
	return items
}

func do(t *testing.T, request *http.Request, wantStatus int) *http.Response {
	t.Helper()
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != wantStatus {
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		t.Fatalf("%s %s: status = %d, want %d; body=%s", request.Method, request.URL, response.StatusCode, wantStatus, body)
	}
	return response
}

func register(t *testing.T, email, password string) string {
	t.Helper()
	response := postJSON(t, "/api/v1/auth/register", map[string]string{"email": email, "password": password}, "", http.StatusCreated)
	defer response.Body.Close()
	var body struct {
		UserID string `json:"user_id"`
	}
	decodeJSON(t, response.Body, &body)
	if body.UserID == "" {
		t.Fatal("register response has an empty user_id")
	}
	return body.UserID
}

func login(t *testing.T, email, password string) string {
	t.Helper()
	response := postJSON(t, "/api/v1/auth/login", map[string]string{"email": email, "password": password}, "", http.StatusOK)
	defer response.Body.Close()
	var body struct {
		AccessToken string `json:"access_token"`
	}
	decodeJSON(t, response.Body, &body)
	if body.AccessToken == "" {
		t.Fatal("login response has an empty access_token")
	}
	return body.AccessToken
}

func createShortURL(t *testing.T, token, originalURL string) string {
	t.Helper()
	response := postJSON(t, "/api/v1/urls", map[string]string{"original_url": originalURL}, token, http.StatusCreated)
	defer response.Body.Close()
	var body struct {
		ShortCode string `json:"short_code"`
	}
	decodeJSON(t, response.Body, &body)
	if body.ShortCode == "" {
		t.Fatal("create URL response has an empty short_code")
	}
	return body.ShortCode
}

func postJSON(t *testing.T, path string, body any, token string, wantStatus int) *http.Response {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, testSystem.gatewayURL+path, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	return do(t, request, wantStatus)
}

func decodeJSON(t *testing.T, reader io.Reader, out any) {
	t.Helper()
	if err := json.NewDecoder(reader).Decode(out); err != nil {
		t.Fatal(err)
	}
}

func uniqueEmail(t *testing.T) string {
	t.Helper()
	return strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-")) + "-" + strconv.FormatInt(time.Now().UnixNano(), 10) + "@example.com"
}
