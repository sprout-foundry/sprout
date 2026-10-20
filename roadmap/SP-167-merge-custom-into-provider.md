# SP-167: Merge `/custom` into `/provider` as subcommands

**Status:** draft
**Origin:** nit #12 from the CLI/WebUI friction list — *"using /custom for adding a
provider isn't intuitive, maybe merge into /provider?"*

## Problem

Registering an OpenAI-compatible endpoint and selecting a provider are two
different verbs, but they live behind two unrelated commands:

- `/provider` — selects among providers: `list`, `select`, `status`,
  `<name>[:<model>]`. Backed by the config manager.
- `/custom` — registers new endpoints: `add` (interactive wizard), `remove`,
  `list`. Shells out to `sprout custom …` so the wizard isn't duplicated.

The split is defensible, but nothing about `/provider` hints that it is where
you go when your endpoint is not listed yet. SP-161g already patched the worst
symptom (the "unknown provider" error now points at `/custom add`), but the
user still has to know a second command exists.

## Decision

**Subcommands under `/provider`; `/custom` becomes a deprecated alias.**
Chosen by the user over a full merge (which would break muscle memory and any
docs/scripts) and over discoverability-only (which leaves the two-command
split intact).

`/provider` becomes the single entry point for everything provider-shaped:

```
/provider                 status (unchanged)
/provider list            list (unchanged)
/provider select          picker (unchanged)
/provider status          status (unchanged)
/provider <name>[:<model>] switch (unchanged)
/provider add             register an OpenAI-compatible endpoint  [NEW]
/provider remove [name]   remove a custom provider                [NEW]
/provider custom          list custom providers                   [NEW]
```

`/custom` keeps working, unchanged, for back-compat.

## Implementation

### 1. Delegate, don't duplicate

`/custom` already shells out to `os.Executable() custom <sub> …` with stdin
attached, which is what lets the interactive wizard run inside the chat. Reuse
that path exactly: `/provider add` calls the same helper. No wizard code moves
into `pkg/agent_commands` (that would be a much larger job and is explicitly
out of scope).

Concretely: extract the shell-out body of `CustomCommand.runSubcommand` into a
package-level function `runCustomSubcommand(sub string, rest []string) error`,
and have both `CustomCommand` and `ProvidersCommand` call it. `CustomCommand`
keeps its own `Execute` switch; `ProvidersCommand` gains three cases that
delegate.

### 2. Dispatch changes in `ProvidersCommand.Execute`

Add to the existing switch:

- `"add"` → `runCustomSubcommand("add", nil)`
- `"remove"`, `"rm"`, `"delete"` → `runCustomSubcommand("remove", args[1:])`
- `"custom"` → `runCustomSubcommand("list", nil)`

Careful: the existing `default:` branch treats an unknown first arg as a
provider *name* to switch to. `add`/`remove`/`custom` must be matched before
that fallthrough, or `/provider add` would try to switch to a provider named
"add". The switch already precedes the default, so this is ordering, not new
logic — but it is the one real correctness risk in the change and needs a test.

### 3. Discoverability

- `ProvidersCommand.Usage()` gains the three new lines.
- `showProviderStatus` and `listProviders` gain a closing hint when custom
  providers exist (or always): `Add an OpenAI-compatible endpoint: /provider add`.
  Prefer showing it always — the user who needs it is precisely the one whose
  provider is *not* in the list.
- `Complete()` adds `add`, `remove`, `custom` to the first-arg subcommand set,
  and `remove` completes custom provider names (mirroring
  `CustomCommand.Complete`).

### 4. Deprecation surfacing

`/custom` stays registered but is marked deprecated in its `Description()`
and `Usage()`: point at `/provider add|remove|custom`. Do **not** print a
deprecation warning on every invocation — `/custom` is still a legitimate
entry point and warning on each use is noise. The alias relationship is
surfaced through `/help custom`.

Note: `/provider` and `/custom` are *not* registry aliases of each other
(they have different subcommand surfaces), so `RegisterAlias` is the wrong
mechanism here. The deprecation is documentation-level.

## Tests

- `/provider add` / `remove` / `custom` dispatch to the custom subcommand, and
  do **not** fall through to `setProvider` (the regression this change could
  introduce). Assert on the error/exit path rather than shelling out for real.
- `/provider <real-provider>` still switches (no regression).
- `Complete()` returns the new subcommands, and `remove` completes custom names.
- Usage text mentions the new subcommands.
- `/custom` still works unchanged.

## Out of scope

- Inlining the wizard into `pkg/agent_commands` (keep the shell-out).
- Removing `/custom` (rejected option 2).
- Any change to `sprout custom` CLI surface.

## Acceptance

- `/provider add`, `/provider remove`, `/provider custom` all work in-chat.
- `/custom add|remove|list` still work.
- `/provider` with no args, `list`, `select`, `status`, `<name>` unchanged.
- `/help provider` documents the new subcommands; `/help custom` marks it
  deprecated and points at `/provider`.
