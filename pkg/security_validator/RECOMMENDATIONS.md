# Security Validator: Command Classification Recommendations

## Risk Levels

| Level | Interactive | Non-Interactive |
|-------|------------|-----------------|
| SAFE | Execute immediately | Execute immediately |
| CAUTION | Ask user to confirm | Auto-allow with log warning |
| DANGEROUS | Ask user to confirm | Block |

## SAFE — No confirmation needed

Read-only and informational operations with no side effects:
- File reads (`read_file`, `search_files`, `cat`, `head`, `tail`, `grep`)
- Directory listing (`ls`, `find`, `tree`, `du`, `df`)
- Git read ops (`git status`, `git log`, `git diff`, `git branch`, `git remote`)
- Build/test (`go build/test/vet/fmt`, `npm test`, `cargo build/test`)
- System info (`ps`, `env`, `uname`, `whoami`, `pwd`)
- Subagent delegation (`run_subagent`, `run_parallel_subagents`)
- Any operation on `/tmp/*`

## CAUTION — User confirms in interactive mode

Operations that modify state but are recoverable:
- **Single file deletion** (without `-rf`): `rm file.txt`
- **Package installs**: `npm install`, `pip install`, `go get`, `cargo add`
- **Git history rewrites**: `git reset`, `git rebase`, `git commit --amend`
- **In-place edits**: `sed -i`
- **Permissions**: `chmod +x`, `chmod 644` (not `777`)
- **Service control**: `systemctl stop` (not `disable`)
- **Build cleanup**: `make clean`, `rm -rf dist/`, `rm -rf build/`
- **Recoverable rm -rf targets**: `node_modules`, `vendor`, `dist`, `build`, `out`, `target`, `bin`, `.next`, `__pycache__`, `.cache`, `.gradle`, `venv`, `.venv`
- **Lock files**: `package-lock.json`, `yarn.lock`, `go.sum`, `Cargo.lock`

## DANGEROUS — Always confirm, always block in non-interactive

Operations that cause permanent loss or security issues:
- **rm -rf on unlisted paths** (any path not in the recoverable list → `src/`, `lib/`, `app/`, arbitrary dirs are DANGEROUS)
- **Git force operations**: `git branch -D`, `git push --force`, `git clean -ffd`
- **Privilege escalation**: `sudo *`, `chmod 777`
- **Arbitrary code execution**: `curl ... | bash`, `wget ... | sh`
- **System destruction**: `mkfs`, `dd` to devices, fork bombs, `eval`
- **System directory writes**: anything targeting `/usr`, `/etc`, `/bin`, `/sbin`, `/var`, `/opt`
- **Version history deletion**: `rm -rf .git`
- **Hard blocks** (no override): `rm -rf /`, `rm -rf .`, `chmod 000 /`, critical config files

## Key Design Decisions

1. **Unknown = Cautious**: Commands that don't match any pattern default to CAUTION (not SAFE)
2. **rm -rf on unlisted paths = DANGEROUS**: The recoverable list is explicit; anything not on it is assumed permanent
3. **Interactive vs non-interactive**: Non-interactive CAUTION auto-allows (with logging) since the agent needs autonomy; only DANGEROUS blocks
4. **Hard blocks override everything**: Critical system ops (e.g., `rm -rf /`) are always blocked regardless of mode
