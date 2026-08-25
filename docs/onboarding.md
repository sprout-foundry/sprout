# Onboarding

Get from zero to a working sprout session in under five minutes. This guide
covers both the **terminal (CLI)** and the **web UI** paths.

> Already configured? Skip to [Changing your provider](#changing-your-provider).

---

## What you need

- **An API key** from one of the supported providers below. Most have a free
  tier or starter credits.
- **A terminal** (macOS/Linux, WSL on Windows, or Git Bash). The web UI works
  in any modern browser.

That's it — no local model downloads required unless you want to use Ollama.

---

## Step 1 — Pick a provider

Sprout talks to LLM providers over standard OpenAI-compatible APIs. You bring
the key; sprout handles the rest.

| Provider | Best for | Get a key |
|---|---|---|
| **Z.AI** | Coding-focused workflows, GLM models | [platform.z.ai](https://platform.z.ai/) |
| **MiniMax** | Large context windows, coding plans | [platform.minimax.io](https://platform.minimax.io/) |
| **OpenRouter** | One key, access to many model families | [openrouter.ai/keys](https://openrouter.ai/keys) |
| **DeepInfra** | Hosted open models, pay-as-you-go | [deepinfra.com](https://deepinfra.com/dash/api_keys) |
| **Chutes** | Low-friction experimentation | [chutes.ai](https://chutes.ai/) |

**Not sure?** Start with **OpenRouter** — one key gives you access to dozens of
models (Claude, Gemini, DeepSeek, GLM, Qwen) so you can experiment freely.

For a full list with live model availability, run:

```bash
sprout keys set   # then pick from the list
```

---

## Step 2 — Add your API key

### Terminal (recommended for first run)

```bash
sprout keys set zai
```

This prompts for your key, **validates it against the live API**, and stores it
in your OS keyring (or encrypted file). Replace `zai` with your chosen provider.

If you don't specify a provider, you'll get an interactive menu.

### Web UI

1. Run `sprout` to start the web UI (opens automatically).
2. The onboarding dialog appears on first run — pick a provider, enter your
   key, and click **Complete Setup**.

### Environment variable (CI / scripts)

```bash
export ZAI_API_KEY=sk-...
sprout agent "explain this codebase"
```

Each provider has its own env var (e.g. `OPENROUTER_API_KEY`,
`DEEPINFRA_API_KEY`). The key is read automatically — no config file needed.

---

## Step 3 — Start working

```bash
# Interactive REPL (recommended — has autocomplete, slash commands)
sprout

# One-shot task
sprout agent "add input validation to the login form"

# Generate a commit message for staged changes
sprout commit
```

In the web UI, just start typing in the chat panel.

You should see the active provider and model in your prompt prefix, e.g.:

```
sprout · zai · glm-5
> _
```

---

## Changing your provider

### Terminal

```bash
# Switch the active provider (must already have a key configured)
export SPROUT_PROVIDER=openrouter

# Set a default model for a provider
sprout config set openrouter.model "qwen/qwen3-coder"

# Add a new key
sprout keys set deepinfra

# Add a custom OpenAI-compatible provider
sprout custom add
```

### Web UI

Open **Settings → Provider** to change providers, models, or update keys.
You can also re-run onboarding from there.

---

## Local models (Ollama)

Want to run everything offline with no API costs? Use [Ollama](https://ollama.com):

```bash
# Install and pull a model (one-time)
ollama pull qwen2.5-coder:7b

# Point sprout at it
export OLLAMA_MODEL=qwen2.5-coder:7b
sprout
```

Ollama providers don't require an API key — just the model name.

---

## Troubleshooting

### "no provider configured"

You haven't set up a key yet. Run `sprout keys set <provider>` or set the
appropriate environment variable (see Step 2).

### "HTTP 401" / "invalid API key"

The key was rejected by the provider. Double-check it's copied correctly with
no trailing spaces, then re-run `sprout keys set <provider>`.

### "model not found"

The model name doesn't exist for your provider/account. Run
`sprout keys set <provider>` to see available models, or check the provider's
documentation.

### Windows: terminal tools don't work

Use **WSL** for the best experience. From a Windows terminal:

```powershell
wsl --install -d Ubuntu
```

Then open your project inside WSL and run sprout there. Git Bash is a lighter
alternative if you only need basic shell commands.

---

## Next steps

- **[CLI Reference](CLI_REFERENCE.md)** — every command and flag
- **[Configuration](CONFIGURATION.md)** — config files, env vars, profiles
- **[Provider Catalog](PROVIDER_CATALOG.md)** — all supported providers and models
- **[Personas](PERSONAS.md)** — coder, reviewer, researcher, and more
- Type `/help` inside the sprout REPL for in-session commands
