// entities.ts — the document families a workspace is made of. The model
// (defaults + the `entities:` section of .specquill/config.yml) lives in
// config.ts; this module keeps the family-specific entry points. Workspaces
// add families, override single fields of a built-in, or remove one:
//
//   entities:
//     decision: { group: why, folder: "decisions/", label: "Decisions", icon: "◆",
//                 color: "#7c5cd6", description: "Why the system is shaped this way." }
//     diagram:  { hidden: true }

import { workspaceConfig } from './config';
import type { EntityDef } from './config';

export { BUILTIN_ENTITIES } from './config';
export type { EntityDef } from './config';

/**
 * Effective entity families: built-ins, overridden/extended/hidden by the
 * workspace config's `entities:` block.
 */
export function parseEntities(configYml: string): EntityDef[] {
  return workspaceConfig(configYml).entities;
}
