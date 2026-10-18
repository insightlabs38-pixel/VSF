# Agent prompts for VSF implementation

- [First batch: B00 bootstrap and execution](01_Bootstrap_B00_Batch_Prompt.md)
- [Keep going: select earliest eligible batch](02_Keep_Going_Prompt.md)

Use the first prompt once to bootstrap the actual repository, import the queue and execute B00. On each subsequent agent run, use the second prompt. Both intentionally authorize **one batch only** per invocation and require hard gate checks, durable task receipts and concise reporting. If a batch is incomplete, the next invocation resumes it rather than assuming it passed.

Required identity: `insightlabs38-pixel` / `insightlabs38@gmail.com`. Never append AI attribution/link footers, never exceed message/report/comment limits. The repository queue is authoritative after bootstrap; the planning ZIP is a source reference.
