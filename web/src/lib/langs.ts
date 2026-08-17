import type { Extension } from '@codemirror/state';
import { StreamLanguage, StreamParser } from '@codemirror/language';
import { c, cpp, csharp, dart, java, kotlin, objectiveC, scala } from '@codemirror/legacy-modes/mode/clike';
import { css } from '@codemirror/legacy-modes/mode/css';
import { go } from '@codemirror/legacy-modes/mode/go';
import { javascript, json, typescript } from '@codemirror/legacy-modes/mode/javascript';
import { properties } from '@codemirror/legacy-modes/mode/properties';
import { protobuf } from '@codemirror/legacy-modes/mode/protobuf';
import { python } from '@codemirror/legacy-modes/mode/python';
import { ruby } from '@codemirror/legacy-modes/mode/ruby';
import { rust } from '@codemirror/legacy-modes/mode/rust';
import { shell } from '@codemirror/legacy-modes/mode/shell';
import { standardSQL } from '@codemirror/legacy-modes/mode/sql';
import { swift } from '@codemirror/legacy-modes/mode/swift';
import { toml } from '@codemirror/legacy-modes/mode/toml';
import { html, xml } from '@codemirror/legacy-modes/mode/xml';

// extension → highlighter for source-code files the editor renders read-only;
// the md/yaml first-class languages keep their dedicated @codemirror packages
const CODE_MODES: Record<string, StreamParser<unknown>> = {
  kt: kotlin, kts: kotlin,
  java: java, groovy: java, gradle: java,
  go: go,
  js: javascript, mjs: javascript, cjs: javascript, jsx: javascript,
  ts: typescript, tsx: typescript,
  json: json,
  py: python,
  rb: ruby,
  rs: rust,
  c: c, h: c,
  cpp: cpp, cc: cpp, hpp: cpp, cxx: cpp,
  cs: csharp,
  m: objectiveC,
  scala: scala, sbt: scala,
  dart: dart,
  swift: swift,
  sh: shell, bash: shell, zsh: shell,
  sql: standardSQL,
  proto: protobuf,
  toml: toml,
  properties: properties, env: properties, ini: properties,
  css: css,
  xml: xml, xsd: xml, xslt: xml,
  html: html, htm: html, vue: html, svelte: html,
};

// own-property check: extensions come from arbitrary filenames (reference
// repos included), and `in`/plain indexing would also hit prototype keys
// like "constructor"
const hasMode = (ext: string) => Object.prototype.hasOwnProperty.call(CODE_MODES, ext.toLowerCase());

export function isCodeExt(ext: string): boolean {
  return hasMode(ext);
}

/** CodeMirror language extension for a source-code file extension, if known. */
export function codeLang(ext: string): Extension | null {
  return hasMode(ext) ? StreamLanguage.define(CODE_MODES[ext.toLowerCase()]) : null;
}
