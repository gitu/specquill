# Open Knowledge Format (OKF) support

specquill workspaces are conformant [OKF v0.1](https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md)
bundles: a directory of markdown files with YAML frontmatter in git, readable
by humans and agents with no specquill tooling required. OKF is a strict subset
of what specquill already does — the typed traceability links (`implements`,
`maps_to`, `verifies`, `drivers`) ride along as producer extension fields,
which the spec requires consumers to preserve.

## Producer

- **`type:` frontmatter** — the only field OKF requires. `specquill init`
  scaffolds it everywhere (including `.specquill/skills/*` and READMEs), the
  in-app "New file" flow derives it from the folder family
  (`requirements/` → `Requirement`, …), and `okf.Validate` checks a tree.
- **Opt-in marker** — a bundle opts into derived-file generation by declaring
  `okf_version` in the frontmatter of its root `index.md`. Scaffolded
  workspaces opt in from commit zero; existing workspaces opt in by adding
  that file.
- **Derived reserved files, regenerated at commit time** (`internal/okf` +
  the hook in `gitx.Commit`): one `index.md` per directory of concepts
  (grouped listings with titles/descriptions from frontmatter) and `log.md`
  (date-grouped change history from git, including the commit being made —
  the derived files always land in the same commit as the change they
  describe). Generation is byte-stable, best-effort, and never blocks a
  commit. Merge commits appear in `log.md` on the next commit after them.

## Consumer

- Untyped OKF body links (`[text](/path/doc.md)`, relative links too) are
  extracted into the workspace model as `references` edges and drawn dashed
  in the traceability graph — semantics stay in the prose, per the spec.
- Reserved files (`index.md`, `log.md`) are never treated as concepts by the
  model, and links found in them create no edges.
- Any external OKF bundle can be mounted as a **read-only reference repo**
  today (it's just markdown); the copilot grounds on it like any other input.

## Notes

- `type` values are deliberately free-form (spec: no central registry);
  specquill uses `Requirement`, `Specification`, `Regulation`, `Data Mapping`,
  `Change Record`, `Decision`, `Glossary`, `Guide`, `Skill`.
- `resource:` (a URI for the underlying asset) is scaffolded on data
  mappings — point it at the real table/system the mapping describes.
- Strict conformance covers *hidden directories too*, which is why the
  `.specquill/skills/` files carry frontmatter.
