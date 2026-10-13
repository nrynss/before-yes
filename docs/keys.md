# Keys

Before Yes uses four keys. Each one lives in `/home/nryn/work/before-yes/.env`, and the settings file names it by variable. The settings file (`before-yes.toml`, copied from `before-yes.toml.example`) holds references only.

| Key | Where to create it | Env var in `.env` | Settings key that references it | In `.env`? |
|---|---|---|---|---|
| Token Factory (Nebius), the paid reader and judge | tokenfactory.nebius.com | `NEBIUS_KEY` | `[secrets.reader]` and `[secrets.judge]` | yes |
| Tavily, free tier (1,000 credits a month) | app.tavily.com | `TAVILY_KEY` | `[secrets.tavily]` | yes |
| OpenRouter, free development stand-ins | openrouter.ai/keys | `OPENROUTER_KEY` | none by default. If the commented OpenRouter tables in `before-yes.toml` are enabled, `[secrets.reader]` and `[secrets.judge]` must point `var` at `OPENROUTER_KEY`. | yes |
| build.nvidia.com, optional second free development host | build.nvidia.com | `NVIDIA_KEY` | none. No settings key references it yet. | yes |

## Rules

- `.env` must stay mode `0600`. Check with `stat -c %a .env`.
- `.env` is gitignored. Do not force-add it.
- `.env` holds no quotes around values and no `export` prefixes. Keel's `env_file` source expects bare `NAME=value` lines.
- Never paste a key into chat, a commit message, a log, or a file in the repository.
- Settings files hold references (`source`, `path`, `var`), never values.
