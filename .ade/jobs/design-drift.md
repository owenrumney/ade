---
name: design-drift
description: Design docs that no longer describe the code they design
schedule: 0 8 * * 1
continuity: summary
allow: reads
---

Read every document under `.git/ade/docs/`. For each one, check the parts of the
codebase it describes — the packages, files, types and constants it names — and
report only where the document and the code have parted company.

Worth reporting:

- A name the document relies on that no longer exists, or was renamed.
- Behaviour the document states that the code contradicts.
- A section describing work marked as not built, which now is.

Not worth reporting: wording, tone, formatting, or a document that is merely
older than the code but still true.

Report as one bullet per drift: the document, the claim, and what the code does
instead, with a file:line. If every document still holds, say so and stop.
