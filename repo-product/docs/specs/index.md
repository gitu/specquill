---
okf_version: "0.1"
---

# Index

## decisions

- [Postgres as the metadata store](decisions/ADR-001.md) *(superseded)*
- [Content roots map server-side](decisions/ADR-002.md)
- [The server is a dumb CRDT relay](decisions/ADR-003.md)
- [Non-git sources become mirror repositories](decisions/ADR-004.md)
- [Embedded SQLite as the metadata store](decisions/ADR-005.md)

## glossary

- [Glossary](glossary/glossary.md)

## requirements

- [Protected default branch](requirements/REQ-001.md)
- [Byte-fidelity editing](requirements/REQ-002.md)
- [Projects in repository subfolders](requirements/REQ-003.md)
- [Multi-stage source authorization](requirements/REQ-004.md)
- [Conformant OKF bundles](requirements/REQ-005.md)
- [Real-time collaborative editing](requirements/REQ-006.md)
- [Grounded AI copilot](requirements/REQ-007.md)
- [Conflict-checked merges to the default branch](requirements/REQ-008.md)
- [External source importers](requirements/REQ-009.md)
- [Portable diagrams and sketches](requirements/REQ-010.md)
- [Project-scoped shareable URLs](requirements/REQ-011.md)
- [Self-describing, extensible entity model](requirements/REQ-012.md)
- [Verifiable link integrity](requirements/REQ-013.md)
- [Traceable document lifecycle](requirements/REQ-014.md)
- [Guided document creation with collision-free IDs](requirements/REQ-015.md)
- [Unauthenticated OKF-bundle share links](requirements/REQ-016.md)
- [GitHub sign-in with gated access and admin bootstrap (deprecated)](requirements/REQ-017.md)
- [Instant sync via push webhooks (deprecated)](requirements/REQ-018.md)
- [GitHub-App tenants with derived authorization (deprecated)](requirements/REQ-019.md)

## specs

- [Content roots — subfolder projects](specs/content-root.md)
- [References — sources, grants, grounding](specs/references.md)
- [Workspace branches — protected main mechanics](specs/workspace-branches.md)
- [Co-editing — collaborative rooms](specs/co-editing.md)
- [Copilot grounding — context and limits](specs/copilot-grounding.md)
- [Merging — landing workspace branches](specs/merging.md)
- [Importers — mirroring non-git sources](specs/importers.md)
- [Diagrams and sketches — portable formats](specs/diagrams.md)
- [URLs — project-scoped deep links](specs/urls.md)
- [Entity model — document families](specs/entity-model.md)
- [Links — resolution and verification](specs/links.md)
- [Document lifecycle — moves and history](specs/document-lifecycle.md)
- [Document creation — guided flow and ID schemes](specs/document-creation.md)
- [Share links — unauthenticated OKF-bundle downloads](specs/share-links.md)
- [Authentication — providers, deployment roles](specs/authentication.md)
- [Webhooks — push-triggered repository sync (deprecated)](specs/webhooks.md)
- [Tenants — GitHub App installations, derived roles (deprecated)](specs/tenants.md)
- [Multi-tenancy — architecture and boundaries (deprecated)](specs/multi-tenancy.md)
- [OKF conformance — producer and consumer](specs/okf.md)
