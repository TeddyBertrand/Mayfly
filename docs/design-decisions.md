# Design decisions

Each entry records one decision, the options considered and the consequences. Entries are numbered in order. A replaced decision is marked `superseded by NNN`, not deleted.

## 001. Binary layout: one binary, separate packages
- **Date:** 2026-11-11
- **Status:** accepted
- **Context:** Mayfly has three components (receiver, provisioner, reaper). The repo layout had to be fixed before any code exists. The brief targets a single-command deploy (`terraform apply`) on one small host, and M1 plans an in-process job queue (a channel) between receiver and provisioner.
- **Options considered:**
  - **A. One binary** (`cmd/mayfly`, logic in `internal/receiver`, `internal/provisioner`, `internal/reaper`): one file and one systemd service to deploy, and components talk through channels. Cannot scale or fail per component.
  - **B. Three binaries** (`cmd/receiver`, `cmd/provisioner`, `cmd/reaper`): independent scaling and failure isolation, but needs IPC or a broker between components and three services to install and supervise.
- **Decision:** A. One binary, with each component in its own `internal/` package behind clear boundaries.
- **Consequences:** Simpler deploy and a cheap, fast queue between receiver and provisioner. A crash in one component takes down the whole process, so shutdown and panic handling must be done carefully. Revisit if the components ever need to run on different hosts or scale independently; the package boundaries keep a later split cheap.
