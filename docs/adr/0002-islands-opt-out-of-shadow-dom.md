# 2. Islands opt out of shadow DOM

**Date:** 2026-08-26 · **Status:** accepted

## Context

Islands are Solid components compiled to custom elements with `solid-element`, so the browser drives
their lifecycle: htmx swaps content in and `connectedCallback` fires, swaps it out and
`disconnectedCallback` fires. No `MutationObserver`, no htmx event hooks, no glue.

`solid-element` wraps each element in a shadow root by default, for style isolation. CSS custom
properties inherit through a shadow boundary but stylesheet *rules* do not. With the design system
in a single global sheet, that means an island's `@theme` tokens resolve while `.st`, the state-pill
grammar, and every Tailwind utility do not. Keeping the default therefore means compiling a second
stylesheet into each island's shadow root, and authoring the shared component grammar twice — once
for Go to emit, once for Solid — with nothing to keep the two in step. Shadow DOM also hides its
contents from htmx, which would foreclose an island ever containing a form.

Nothing on this page is third-party. There is no untrusted markup to isolate from, and the app
serves one origin on the operator's own laptop.

## Decision

Every island calls `noShadowDOM()` as its first statement. One global stylesheet styles the whole
page, Go-rendered and Solid-rendered alike.

## Consequences

A state pill is byte-identical whether Go or Solid drew it, from one definition. One Tailwind entry
point, one build output, one place to change a token. htmx can see into an island, so an island may
contain a form if one ever earns its place. In exchange there is no encapsulation: an island's
styles are page styles and can collide, which the semantic-class convention (`.st`, `.tl`, `.seg-h`)
is what guards against. Reverting means giving each island its own compiled sheet and duplicating
the shared grammar, so this gets harder to undo with each island added — revisit only if third-party
or untrusted markup ever mounts on this page.
