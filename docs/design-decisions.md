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

## 002. Develop locally first, cloud host later
- **Date:** 2026-11-11
- **Status:** accepted
- **Context:** Issue #5 asks for a cloud account, a cost alert and a dev host. M1 (receiver, provisioner, Docker worker) only needs Docker and a way for GitHub to reach the webhook endpoint, which a tunnel provides (M1 "Local dev guide with a tunnel"). The real host is built in M2 by Terraform, and the brief flags cloud cost overruns as a risk.
- **Options considered:**
  - **A. Develop locally first:** run Mayfly and its Docker workers on the owner's machine, exposed through a tunnel. Costs nothing and keeps iteration fast, but the cloud environment isn't tested until M2.
  - **B. Hetzner dev host now:** a small cloud server set up by hand. Gives a real environment early, but costs a few euros a month and produces a hand-built server that the M2 Terraform work replaces.
- **Decision:** A. Develop locally until the M2 Terraform issues.
- **Consequences:** No cloud spend during M0 and M1. Code must not assume a cloud host (paths, public IP); configuration comes from environment variables. The Hetzner project, cost alert and SSH key are created at the start of M2, before the first `terraform apply`. Revisit if local Docker can't reproduce something M1 needs, for example the network or resource limits of the target host.
