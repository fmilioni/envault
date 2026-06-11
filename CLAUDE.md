# Envault

CLI em Go que salva/restaura arquivos `.env` — "git stash para variáveis de ambiente". Arquitetura, modelo do cofre e decisões vivem nos docs do claude-organizer (projeto `envault`), não aqui.

## Git

- Mensagens de commit em inglês, um commit por card, com a key no rodapé (ex.: `feat(cli): … (ENV-19)`).

## claude-organizer

- Auth: **on** — os scripts de captura de diff (`attach-commit`/`attach-worktree-diff`) exigem `CO_COMMIT_TOKEN` (mintar via `issue_commit_token`).
