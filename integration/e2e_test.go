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
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
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
	postgres tc.Container
	mongo    tc.Container
	redis    tc.Container
	kafka    tc.Container

	kafkaBroker string
	gatewayURL  string
	processes   []*serviceProcess
}

func TestMain(m *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
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
			hostConfig.PortBindings = nat.PortMap{
				port: []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: strconv.Itoa(hostPort)}},
			}
		},
		WaitingFor: wait.ForListeningPort(port).WithStartupTimeout(containerStartTimeout),
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

	userPort, err := freePort()
	if err != nil {
		return err
	}
	urlPort, err := freePort()
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

	gatewayEnv := map[string]string{
		"GATEWAY_PORT":      strconv.Itoa(gatewayPort),
		"USER_SERVICE_ADDR": userAddress,
		"URL_SERVICE_ADDR":  urlAddress,
	}
	if err := s.startService(root, binaries["gateway"], gatewayEnv, net.JoinHostPort("127.0.0.1", strconv.Itoa(gatewayPort))); err != nil {
		return err
	}
	return nil
}

func buildServices(ctx context.Context, root string) (map[string]string, error) {
	binaries := make(map[string]string, 3)
	dir, err := os.MkdirTemp("", "url-shortener-integration-")
	if err != nil {
		return nil, err
	}
	for name, pkg := range map[string]string{"user": "./user/cmd", "url": "./url/cmd", "gateway": "./gateway/cmd"} {
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
	mapped, err := container.MappedPort(ctx, port)
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
	for _, container := range []tc.Container{s.kafka, s.redis, s.mongo, s.postgres} {
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
