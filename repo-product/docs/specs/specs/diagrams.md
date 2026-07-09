---
type: Specification
title: Diagrams and sketches — portable formats
status: approved
satisfies: [requirements/REQ-010.md]
updated: 2026-07-09
---

# Diagrams and sketches — portable formats

How [REQ-010](../requirements/REQ-010.md) is realized.

## Sketches

A sketch is a PNG with the editable scene embedded in the image. It renders
natively wherever a PNG does — a diff view, a git host, an exported bundle —
and the editor recovers the scene from the same file to allow further edits.
Re-export preserves the scene, so editing is lossless round-trip.

Editing mutates the existing image element in place (swapping its source);
the node is never replaced, which would drop it from the document.

## Text diagrams

Mermaid diagrams are authored inline in fenced code blocks and rendered on
view, so their source stays in the markdown and travels with the document.
