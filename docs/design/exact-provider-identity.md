---
title: Exact provider identity
---

# Exact provider identity

A task records which provider ran it. This document is about making that record
true — what identifies one exact invocation, and why the obvious implementation
of "did the binding honor the request?" got the answer backwards.

## Requested is not the same as ran

A `Request` names a **tier** (`haiku`, `sonnet`, `opus`) and an effort. A
binding maps those onto whatever its CLI actually accepts. Those are different
values, and only the second one describes what happened.

`resolveModel` treats `SonnetModel` as a *universal* fallback, not merely the
sonnet tier's mapping. So a codex binding configured only with
`SonnetModel: "gpt-5"` answers a request for **opus** with `gpt-5` — and
returns no error. Recording "opus" for that turn is not a rounding error; it is
a provenance entry that contradicts the run.

`Invocation` closes that gap:

```go
type Invocation struct {
    Alias    string // which binding
    Provider string // its type: claude, codex, opencode, …
    Model    string // the CONCRETE model, after resolution
    Effort   string // the resolved effort
}
```

`ResolveInvocation` returns it, and every runner reports it on its `Result`. An
empty `Effort` is meaningful rather than missing: it says the binding runs at
its provider's own default.

Loose resolution is unchanged by design. Every existing plan depends on tiers
resolving with fallbacks, so making that strict by default would change what
those plans run. What `Invocation` adds is *visibility*.

## Strict binding, and the check that got it backwards

`Request.StrictBinding` refuses a request the binding cannot honor exactly. It
exists for a task pinned to a specific model, where silently running a different
one defeats the entire point of pinning.

The first implementation asked the wrong question. It inspected the binding's
**config shape** — does this binding declare a mapping for the requested tier? —
and that approved exactly the substitutions strict mode exists to refuse:

- A `claude`-type binding short-circuited to "honorable" for *every* tier, so a
  strict **opus** request against a binding configured only with `SonnetModel`
  ran sonnet while its provenance claimed opus.
- A non-tier model counted as mapped merely for being non-empty, so a strict
  request for `gpt-4` against `SonnetModel: "gpt-5"` ran **gpt-5**.

The reliable question is not what the config looks like but whether the
resolution *substituted*:

```go
// An explicit tier mapping is authoritative when present. Otherwise the
// resolution must have left the request unchanged.
func bindingHonorsModel(cfg BindingConfig, requested Model, resolved string) bool
```

The distinction generalizes: **compare against the resolved value, not against
the shape of the thing that produced it.** A shape-based check can only ever
approximate the behavior it is trying to verify, and here the approximation
inverted on the exact cases that mattered.

An unhonorable request returns `ErrBindingCannotHonorRequest`, which is distinct
from an I/O fault so a caller can tell "this pin cannot be satisfied" from
"something broke".

## Fingerprinting a command line

`InvocationConfigHash` fingerprints the whole binding config plus the exact
model and effort:

```go
func InvocationConfigHash(binding Binding, model Model, effort string) (string, error)
```

It covers the alias, the full `BindingConfig`, and the resolved model/effort.
The direction that holds is the useful one: **equal hashes mean the same
configured invocation.** The converse does not — two aliases pointing at
identical config produce different hashes while the runners build the same argv,
so unequal hashes mean "not known to be the same", not "provably different
command lines".

That asymmetry is deliberate rather than a defect. The hash keys a *measurement
of an alias*, and an operator who splits one binding into two aliases has
created two things to calibrate independently, even where today's argv coincides
— the aliases exist precisely so they can diverge. Treating them as
interchangeable would attribute one alias's evidence to another.

Changing the alias, the args, the model, or the effort all change the hash; the
same inputs hash identically, or every lookup would miss.

This is what makes a calibration reusable. A calibration is a *measurement of
one exact command line*, so it is keyed by this hash rather than by the alias:
an alias whose binary was upgraded underneath it is a different measurement,
and reusing the old one would attribute results to a command line that no
longer exists.

## What this does not do

- **It does not make loose resolution safe.** A plan that does not set
  `StrictBinding` still gets fallbacks. The guarantee is that the *record* says
  what ran, not that what ran was what you would have picked.
- **It does not verify the provider honored the flags.** Ralph knows the argv it
  built. A CLI that ignores `-m` produces a truthful `Invocation` and an
  untruthful run; catching that needs calibration evidence, not resolution.
- **It does not pin the binary.** Two runs of the same alias can hash
  identically across a provider upgrade, because the hash covers configuration
  rather than the executable. `ProviderCalibration` carries the binary path,
  version, and digest for that reason.
