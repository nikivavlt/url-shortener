package service

// func do() {
// for {
//     batch, err := broker.ReadMessages(ctx, 100, 5*time.Second)
//     if err != nil {
//         return err
//     }

//     if len(batch.Events) == 0 {
//         continue
//     }

//     if err := clickhouseRepo.InsertBatch(ctx, batch.Events); err != nil {
//         return err
//     }

//     if err := broker.CommitMessages(ctx, batch.Messages); err != nil {
//         return err
//     }
// }
// }