---
okf_version: "0.1"
---

# Index

## decisions

- [Postgres as the metadata store](decisions/ADR-001.md)
- [Content roots map server-side](decisions/ADR-002.md)
- [The server is a dumb CRDT relay](decisions/ADR-003.md)
- [Non-git sources become mirror repositories](decisions/ADR-004.md)

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
- [Reviewed merges via pull requests](requirements/REQ-008.md)
- [External source importers](requirements/REQ-009.md)
- [Portable diagrams and sketches](requirements/REQ-010.md)
- [Project-scoped shareable URLs](requirements/REQ-011.md)
- [Self-describing, extensible entity model](requirements/REQ-012.md)
- [Verifiable link integrity](requirements/REQ-013.md)
- [Traceable document lifecycle](requirements/REQ-014.md)
- [Guided document creation with collision-free IDs](requirements/REQ-015.md)
- [Unauthenticated OKF-bundle share links](requirements/REQ-016.md)
- [GitHub sign-in with gated access and admin bootstrap](requirements/REQ-017.md)
- [Instant sync via push webhooks](requirements/REQ-018.md)

## specs

- [Content roots — subfolder projects](specs/content-root.md)
- [References — sources, grants, grounding](specs/references.md)
- [Workspace branches — protected main mechanics](specs/workspace-branches.md)
- [Co-editing — collaborative rooms](specs/co-editing.md)
- [Copilot grounding — context and limits](specs/copilot-grounding.md)
- [Pull requests — reviewed merges](specs/pull-requests.md)
- [Importers — mirroring non-git sources](specs/importers.md)
- [Diagrams and sketches — portable formats](specs/diagrams.md)
- [URLs — project-scoped deep links](specs/urls.md)
- [Entity model — document families](specs/entity-model.md)
- [Links — resolution and verification](specs/links.md)
- [Document lifecycle — moves and history](specs/document-lifecycle.md)
- [Document creation — guided flow and ID schemes](specs/document-creation.md)
- [Share links — unauthenticated OKF-bundle downloads](specs/share-links.md)
- [Authentication — providers, access gate, tenant roles](specs/authentication.md)
- [Webhooks — push-triggered repository sync](specs/webhooks.md)
