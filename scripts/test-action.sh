#!/usr/bin/env bash
# Test the composite action's shell steps (action.yml) locally against a bare git repo.
# Run from the repo root: bash scripts/test-action.sh
set -euo pipefail

# The harness injects a push-blocking remote helper for any remote named "origin"
# through GIT_CONFIG_* environment variables. This test only ever pushes to a
# throwaway bare repo it creates itself, so drop that injection for the run.
while IFS= read -r line; do unset "${line%%=*}"; done < <(env | grep '^GIT_CONFIG_' || true)

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ACTION_YML="$ROOT/action.yml"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

pass=0
fail=0
ok()  { echo "PASS: $1"; pass=$((pass + 1)); }
bad() { echo "FAIL: $1"; fail=$((fail + 1)); }

# extract_run NAME -> writes the step's run: script to stdout.
extract_run() {
  python3 -c '
import sys, yaml
name, path = sys.argv[1], sys.argv[2]
doc = yaml.safe_load(open(path))
for s in doc["runs"]["steps"]:
    if s.get("name") == name:
        sys.stdout.write(s["run"])
        break
else:
    sys.exit("no step named " + name)
' "$1" "$ACTION_YML"
}

# run_step NAME ENV=VAL... runs the step the way GitHub does (bash -e -o pipefail)
# and records the exit code in STEP_CODE, stdout in STEP_OUT, stderr in STEP_ERR.
STEP_CODE=0
STEP_OUT=""
STEP_ERR=""
run_step() {
  local name="$1"; shift
  local script="$TMP/step-$name.sh"
  extract_run "$name" > "$script"
  STEP_OUT="$TMP/$name.out"
  STEP_ERR="$TMP/$name.err"
  STEP_CODE=0
  env "$@" bash -e -o pipefail "$script" > "$STEP_OUT" 2> "$STEP_ERR" || STEP_CODE=$?
}

# get_out FILE KEY -> value of the last "KEY=value" line in a GITHUB_OUTPUT file.
get_out() {
  awk -v k="$2" 'index($0, k "=") == 1 { v = substr($0, length(k) + 2) } END { print v }' "$1"
}

contains() { grep -qF -- "$2" "$1"; }

# ---- build the CLI once and make a signing key ------------------------------
BIN="$TMP/gitpin"
( cd "$ROOT" && go build -o "$BIN" ./cmd/gitpin )
KEY="$("$BIN" keygen | sed -n 's/^GITPIN_KEY=//p')"
BASE_URL_TEST="http://127.0.0.1:8099"

# ---- check step -------------------------------------------------------------
OUT="$TMP/check.out"

: > "$OUT"
run_step check KEY="$KEY" BASE_URL_INPUT="" GITHUB_REPOSITORY="alice/alice.github.io" GITHUB_REPOSITORY_OWNER="alice" GITHUB_OUTPUT="$OUT"
if [ "$STEP_CODE" -eq 0 ] && [ "$(get_out "$OUT" base_url)" = "https://alice.github.io" ]; then
  ok "check: default base_url for <owner>.github.io"
else
  bad "check: default base_url (code=$STEP_CODE, base_url=$(get_out "$OUT" base_url))"
fi

: > "$OUT"
run_step check KEY="$KEY" BASE_URL_INPUT="" GITHUB_REPOSITORY="alice/other" GITHUB_REPOSITORY_OWNER="alice" GITHUB_OUTPUT="$OUT"
if [ "$STEP_CODE" -ne 0 ] && contains "$STEP_OUT" "needs a repo named"; then
  ok "check: non-pages repo without base-url fails"
else
  bad "check: non-pages repo without base-url (code=$STEP_CODE)"
fi

: > "$OUT"
run_step check KEY="$KEY" BASE_URL_INPUT="https://pins.example.com" GITHUB_REPOSITORY="alice/other" GITHUB_REPOSITORY_OWNER="alice" GITHUB_OUTPUT="$OUT"
if [ "$STEP_CODE" -eq 0 ] && [ "$(get_out "$OUT" base_url)" = "https://pins.example.com" ]; then
  ok "check: non-pages repo with base-url passes"
else
  bad "check: non-pages repo with base-url (code=$STEP_CODE)"
fi

: > "$OUT"
run_step check KEY="" BASE_URL_INPUT="" GITHUB_REPOSITORY="alice/alice.github.io" GITHUB_REPOSITORY_OWNER="alice" GITHUB_OUTPUT="$OUT"
if [ "$STEP_CODE" -ne 0 ] && contains "$STEP_OUT" "GITPIN_KEY secret is not set"; then
  ok "check: empty key fails"
else
  bad "check: empty key (code=$STEP_CODE)"
fi

# ---- site + publish + push --------------------------------------------------
# setup_repo DIR creates a bare origin and a workspace clone with pins/hello.txt.
setup_repo() {
  local d="$1"
  mkdir -p "$d/rt"
  git init --bare -q "$d/origin.git"
  git clone -q "$d/origin.git" "$d/ws"
  git -C "$d/ws" config user.email "bot@example.com"
  git -C "$d/ws" config user.name "bot"
  mkdir -p "$d/ws/pins"
  printf 'hello world\n' > "$d/ws/pins/hello.txt"
  git -C "$d/ws" add -A
  git -C "$d/ws" commit -qm init
  git -C "$d/ws" push -q origin "HEAD:refs/heads/main"
  ln -s "$BIN" "$d/rt/gitpin"
}

# run_cycle DIR CONTENT BASE_URL DATE runs site+publish+push, setting HEAD,
# PEER_ID, SITE_DIR. CYCLE_ERR holds a reason on failure.
CYCLE_ERR=""
run_cycle() {
  local d="$1" content="$2" baseurl="$3" date="$4"
  local rt="$d/rt" ws="$d/ws"
  rm -rf "$rt/gitpin-site" "$rt/gitpin.index"

  : > "$rt/site.out"
  run_step site BRANCH="gh-pages" GITHUB_WORKSPACE="$ws" RUNNER_TEMP="$rt" GITHUB_OUTPUT="$rt/site.out"
  if [ "$STEP_CODE" -ne 0 ]; then CYCLE_ERR="site exit $STEP_CODE: $(cat "$STEP_ERR")"; return 1; fi
  SITE_DIR="$(get_out "$rt/site.out" dir)"

  : > "$rt/pub.out"
  run_step publish GITPIN_KEY="$KEY" CONTENT="$content" BASE_URL="$baseurl" SITE="$SITE_DIR" GITHUB_WORKSPACE="$ws" RUNNER_TEMP="$rt" GITHUB_OUTPUT="$rt/pub.out"
  if [ "$STEP_CODE" -ne 0 ]; then CYCLE_ERR="publish exit $STEP_CODE: $(cat "$STEP_ERR")"; return 1; fi
  HEAD="$(get_out "$rt/pub.out" head)"
  PEER_ID="$(get_out "$rt/pub.out" peer-id)"

  run_step push BRANCH="gh-pages" SITE="$SITE_DIR" HEAD_CID="$HEAD" GITHUB_WORKSPACE="$ws" RUNNER_TEMP="$rt" GIT_AUTHOR_DATE="$date" GIT_COMMITTER_DATE="$date"
  if [ "$STEP_CODE" -ne 0 ]; then CYCLE_ERR="push exit $STEP_CODE: $(cat "$STEP_ERR")"; return 1; fi
  return 0
}

S1="$TMP/s1"
setup_repo "$S1"
ORIGIN="$S1/origin.git"

# first run: no gh-pages yet
if run_cycle "$S1" pins "$BASE_URL_TEST" "2024-01-01T00:00:00Z"; then
  reason=""
  if ! git --git-dir="$ORIGIN" rev-parse --verify -q refs/heads/gh-pages > /dev/null; then
    reason="no gh-pages branch on origin"
  elif [ -n "$(git --git-dir="$ORIGIN" show -s --pretty=%P refs/heads/gh-pages)" ]; then
    reason="gh-pages commit has a parent"
  else
    tree="$(git --git-dir="$ORIGIN" ls-tree -r --name-only refs/heads/gh-pages)"
    for f in .nojekyll index.html gitpin.json ipfs/bafkqaaa ipni/v1/ad/head; do
      if ! printf '%s\n' "$tree" | grep -qxF "$f"; then reason="tree missing $f"; break; fi
    done
  fi
  if [ -z "$reason" ] && [ -z "$HEAD" ]; then reason="empty head output"; fi
  if [ -z "$reason" ] && [ -z "$PEER_ID" ]; then reason="empty peer-id output"; fi
  if [ -z "$reason" ]; then ok "site+publish+push first run: parentless gh-pages with expected tree"; else bad "first run: $reason"; fi
else
  bad "first run cycle: $CYCLE_ERR"
fi

# CNAME is kept across a publish
git -C "$S1/ws" fetch -q origin "refs/heads/gh-pages:refs/heads/gh-pages"
git -C "$S1/ws" checkout -q gh-pages
printf 'example.com\n' > "$S1/ws/CNAME"
git -C "$S1/ws" add CNAME
git -C "$S1/ws" commit -qm "add CNAME"
git -C "$S1/ws" push -q origin "gh-pages:refs/heads/gh-pages"
git -C "$S1/ws" checkout -q main
if run_cycle "$S1" pins "$BASE_URL_TEST" "2024-01-02T00:00:00Z"; then
  reason=""
  if [ "$(git --git-dir="$ORIGIN" show refs/heads/gh-pages:CNAME 2>/dev/null)" != "example.com" ]; then
    reason="CNAME not preserved"
  elif [ -n "$(git --git-dir="$ORIGIN" show -s --pretty=%P refs/heads/gh-pages)" ]; then
    reason="gh-pages commit has a parent"
  fi
  if [ -z "$reason" ]; then ok "CNAME kept and commit still parentless"; else bad "CNAME: $reason"; fi
else
  bad "CNAME cycle: $CYCLE_ERR"
fi

# always pushes: unchanged content -> new commit, same tree
prev_commit="$(git --git-dir="$ORIGIN" rev-parse refs/heads/gh-pages)"
prev_tree="$(git --git-dir="$ORIGIN" rev-parse "refs/heads/gh-pages^{tree}")"
if run_cycle "$S1" pins "$BASE_URL_TEST" "2024-01-03T00:00:00Z"; then
  new_commit="$(git --git-dir="$ORIGIN" rev-parse refs/heads/gh-pages)"
  new_tree="$(git --git-dir="$ORIGIN" rev-parse "refs/heads/gh-pages^{tree}")"
  if [ "$new_commit" != "$prev_commit" ] && [ "$new_tree" = "$prev_tree" ]; then
    ok "always pushes: new commit SHA, same tree"
  else
    bad "always pushes: commit $prev_commit->$new_commit tree $prev_tree->$new_tree"
  fi
else
  bad "always-pushes cycle: $CYCLE_ERR"
fi

# ---- missing content warning ------------------------------------------------
S2="$TMP/s2"
setup_repo "$S2"
if run_cycle "$S2" pins "$BASE_URL_TEST" "2024-02-01T00:00:00Z"; then
  : > "$S2/rt/miss.out"
  run_step publish GITPIN_KEY="$KEY" CONTENT="nope" BASE_URL="$BASE_URL_TEST" SITE="$S2/rt/gitpin-site" GITHUB_WORKSPACE="$S2/ws" RUNNER_TEMP="$S2/rt" GITHUB_OUTPUT="$S2/rt/miss.out"
  if [ "$STEP_CODE" -eq 0 ] && contains "$STEP_OUT" "::warning::" && contains "$STEP_OUT" "nope"; then
    ok "missing content folder prints ::warning:: naming it"
  else
    bad "missing content (code=$STEP_CODE): $(cat "$STEP_OUT")"
  fi
else
  bad "missing-content setup cycle: $CYCLE_ERR"
fi

# ---- wait-indexed exit mapping (stub binary) --------------------------------
S3="$TMP/s3"
mkdir -p "$S3/rt/site"
printf '#!/usr/bin/env bash\nexit 2\n' > "$S3/rt/gitpin"
chmod +x "$S3/rt/gitpin"
run_step wait-indexed SITE="$S3/rt/site" INDEXER="http://127.0.0.1:1" RUNNER_TEMP="$S3/rt"
if [ "$STEP_CODE" -eq 0 ] && contains "$STEP_OUT" "::warning::"; then
  ok "wait-indexed: exit 2 -> warning, step exits 0"
else
  bad "wait-indexed exit 2 (code=$STEP_CODE)"
fi

printf '#!/usr/bin/env bash\nexit 1\n' > "$S3/rt/gitpin"
run_step wait-indexed SITE="$S3/rt/site" INDEXER="http://127.0.0.1:1" RUNNER_TEMP="$S3/rt"
if [ "$STEP_CODE" -eq 1 ]; then
  ok "wait-indexed: exit 1 -> step exits 1"
else
  bad "wait-indexed exit 1 (code=$STEP_CODE)"
fi

# ---- summary ----------------------------------------------------------------
S4="$TMP/s4"
mkdir -p "$S4/rt"
cat > "$S4/rt/publish.json" <<'JSON'
{"changed":true,"head":"bafyhead","peer_id":"12D3peer","roots":[{"name":"a|b","cid":"bafyroot1"},{"name":"plain","cid":"bafyroot2"}]}
JSON
: > "$S4/rt/summary.md"
run_step summary SITE="$S4/rt/site" RUNNER_TEMP="$S4/rt" GITHUB_STEP_SUMMARY="$S4/rt/summary.md"

cat > "$TMP/sumcheck.py" <<'PY'
import sys
path, nroots = sys.argv[1], int(sys.argv[2])
lines = open(path).read().splitlines()
table = [l for l in lines if l.startswith("|")]
def is_sep(l): return set(l) <= set("| -")
def is_hdr(l): return "Name" in l and "CID" in l and "Links" in l
def pipes(l):
    return sum(1 for i, c in enumerate(l) if c == "|" and (i == 0 or l[i-1] != "\\"))
data = [l for l in table if not is_sep(l) and not is_hdr(l)]
errs = []
if not any(is_hdr(l) for l in table):
    errs.append("no header row")
if len(data) != nroots:
    errs.append("expected %d data rows, got %d" % (nroots, len(data)))
for d in data:
    if pipes(d) != 4:
        errs.append("row has %d unescaped pipes: %r" % (pipes(d), d))
if nroots >= 1 and not any("a\\|b" in d for d in data):
    errs.append("no row with escaped a|b")
if errs:
    sys.stderr.write("; ".join(errs))
    sys.exit(1)
PY

if msg="$(python3 "$TMP/sumcheck.py" "$S4/rt/summary.md" 2 2>&1)"; then
  ok "summary: table with header + one row per root, 4 unescaped pipes each"
else
  bad "summary table: $msg"
fi

cat > "$S4/rt/publish.json" <<'JSON'
{"changed":false,"head":"","peer_id":"12D3peer","roots":[]}
JSON
: > "$S4/rt/summary.md"
run_step summary SITE="$S4/rt/site" RUNNER_TEMP="$S4/rt" GITHUB_STEP_SUMMARY="$S4/rt/summary.md"
if contains "$S4/rt/summary.md" "Nothing pinned"; then
  ok "summary: zero roots writes Nothing pinned line"
else
  bad "summary zero roots"
fi

# The announce step is skipped: it needs a live indexer (cid.contact).

echo "----"
echo "passed=$pass failed=$fail"
[ "$fail" -eq 0 ]
