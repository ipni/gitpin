---
name: gitpin
description: Pin (publish) files to IPFS with no IPFS node, using ipni/gitpin: a GitHub Action that serves content-addressed blocks from GitHub Pages and indexes them on cid.contact. Use when the user asks to pin, publish or get a CID for a file on IPFS and has the GitHub CLI (`gh`) available.
---

# Pin files to IPFS with gitpin

gitpin turns every top-level file or folder in a repo's `pins/` directory into an
IPFS CID. On push, a GitHub Action builds the blocks and pushes them to `gh-pages`.
GitHub Pages then serves them as a trustless gateway, and cid.contact indexes them so
any IPFS client can find them. You don't need an IPFS node or a pinning-service account.

**Goal: exactly one gitpin run per pin, and it succeeds.** (GitHub's own "pages build and deployment" runs also appear in the Actions tab; they are not gitpin runs.) Starting from the template pushes a
sample `pins/hello.txt`, which starts a run before Pages is set up. Instead, set
everything up before the first push to `main`. That push is the only one that triggers a run.

## Before you start

- You need `gh` logged in (`gh auth status`) with `repo` and `workflow` scopes, plus `git` and `ssh-keygen`.
- Everything pinned is **public** and listed on cid.contact. Don't pin secrets or
  personal data; if a file looks sensitive, ask first.
- The site must be at a domain root, so use a repo named exactly `<owner>.github.io`
  (owner can be a user or an org), or any repo with a custom domain (the `base-url` input).
  If `<user>.github.io` already hosts a website, don't overwrite it; use an org or a
  custom domain instead.

```sh
O=<owner>; R=$O/$O.github.io; W=$(mktemp -d)
```

## 1. Existing gitpin site? Skip to step 5

```sh
gh api repos/$R/contents/.github/workflows/gitpin.yml -q .name >/dev/null 2>&1 && gh secret list -R $R   # no output: not set up yet
```

If `gitpin.yml` and the `GITPIN_KEY` secret both exist, go to step 5.

## 2. Create an empty repo and its key

Don't use `--template`. Creating from the template pushes a sample `pins/hello.txt`
to `main`, which starts a run you don't want.

```sh
gh repo create $R --public
ssh-keygen -q -t ed25519 -C gitpin -N "" -f "$W/key"       # no passphrase: required
gh secret set GITPIN_KEY -R $R < "$W/key"
rm -f "$W/key" "$W/key.pub"                                 # the secret is the only copy
```

Make a new key just for gitpin. Never reuse the user's own SSH key.

## 3. Create `gh-pages` and serve Pages from it

gitpin needs Pages to serve the head it just pushed. If Pages serves another branch or
folder, or deploys from Actions, the run stops right after its push with
`GitHub Pages serves ... but gitpin pushed to ...`. On a branch with no `gitpin.json`,
gitpin starts a fresh site (keeping only `CNAME`), so a `gh-pages` holding only
`.nojekyll` is enough.

```sh
git init -q "$W/pages" && cd "$W/pages"
git checkout -q --orphan gh-pages && touch .nojekyll && git add .nojekyll
git commit -qm "init gh-pages" && git push -q https://github.com/$R.git gh-pages
gh api -X POST repos/$R/pages -f 'source[branch]=gh-pages' -f 'source[path]=/' >/dev/null 2>&1 \
  || gh api -X PUT repos/$R/pages -f 'source[branch]=gh-pages' -f 'source[path]=/'
gh api repos/$R/pages -q .source.branch                     # must print gh-pages
```

POST turns Pages on (and starts a build of the placeholder branch, which is harmless);
PUT changes the branch if Pages was already on, and may not start a build. The run's own
push to `gh-pages` comes later and starts the build that matters. If you switch the
source after gitpin has already pushed (for example after a run failed with the Pages
error), also request a build, or the site can stay 404:

```sh
gh api -X POST repos/$R/pages/builds
```

## 4. Push the workflow and the files together

```sh
git init -q -b main "$W/site" && cd "$W/site" && mkdir -p .github/workflows pins
gh api -H 'Accept: application/vnd.github.raw' repos/ipni/gitpin-template/contents/.github/workflows/gitpin.yml > .github/workflows/gitpin.yml
cp /path/to/file pins/
git add -A && git commit -qm "Pin <file>" && git push -q https://github.com/$R.git main
gh repo edit $R --default-branch main      # gh-pages was pushed first, so it became the default
```

This push triggers the single run. Keep the workflow exactly as fetched: its
`concurrency` group and default checkout credentials are required, and its `pages: read`
permission is what lets a wrong Pages setting fail at once instead of after a 10-minute wait.

## 5. Pinning into an existing site

```sh
git clone -q -b main https://github.com/$R.git "$W/site" && cd "$W/site"
cp /path/to/file pins/ && git add pins && git commit -qm "Pin <file>" && git push -q
```

- Each top-level entry in `pins/` gets one CID. A folder gets one directory CID and nothing more: files inside it get no entries of their own in `gitpin.json` or the run summary. Reach them as `<cid>/path` through an IPFS client (see step 7). This is expected and doesn't need investigating. If the user wants a CID per file, put each file at the top level of `pins/` instead.
- Names starting with `.` are skipped, and symlinks fail the run.
- Editing a file gives it a new CID, and deleting it unpublishes it. Never delete `pins/`, because that unpublishes everything.
- To pin several files, put them in one commit so they go out in one run.

## 6. Watch the run

A run usually takes about a minute: it builds, waits for Pages to serve the new head,
announces, then polls cid.contact (every 15 seconds, up to 15 minutes) until every pin
is listed; cid.contact often lists it on the first poll. Report progress instead
of blocking silently:

```sh
gh run list -R $R --workflow gitpin.yml -L 1        # wait a few seconds after the push for it to appear
gh run watch <id> -R $R --exit-status
```

The yellow warning "cid.contact has not indexed every pin yet" is not a failure. The
file is published, and the index catches up.

## 7. Read the CID, verify, report

```sh
curl -s https://$O.github.io/gitpin.json | jq -r '.roots[] | "\(.name) \(.cid)"'   # with a custom domain, use the base-url host
curl -s https://$O.github.io/ipfs/<cid>              # a file of 256 KiB or less is one raw block, returned as-is
curl -s https://cid.contact/cid/<cid> | jq .          # provider record once indexed
```

For a folder pin, `curl https://$O.github.io/ipfs/<cid>` returns the directory's root
block (binary, a 200 means it's published), and `<cid>/file` returns 404 on plain HTTP.
Pages only serves blocks by CID and can't walk a directory, so that 404 is expected and
not a failure. Verify the folder with the 200 on the root block plus the cid.contact
record, and stop there. Don't compute CIDs yourself (hashing files, encoding base32) or
check files inside a folder one by one. `gitpin.json` and the run summary are the only
source of CIDs, and a successful run means every block was published. A locally computed
CID that 404s usually means the local file changed after it was pinned, or a different
chunking or encoding than gitpin's, not a broken pin. Report the CIDs and finish. For file links, give `https://inbrowser.link/ipfs/<cid>/<path>` or
`ipfs cat <cid>/<path>`, which do the lookup in the client.

Give the user the CID, `ipfs://<cid>`, `https://inbrowser.link/ipfs/<cid>` and
`https://cid.contact/cid/<cid>`. Add one caveat: only their Pages site serves the
file. For durability, they should also pin it elsewhere (`ipfs pin add <cid>`, or a
pinning service).

## Troubleshooting

| Run error | Fix |
| --- | --- |
| `is not serving the new head yet` | Pages hasn't deployed yet, isn't on (step 3), the repo is private, or the repo name is wrong (workflows without `pages: read` also land here for a wrong branch). Fix it, run `gh api -X POST repos/$R/pages/builds`, then `gh run rerun <id> -R $R` |
| `GitHub Pages serves ... but gitpin pushed to ...` | Pages points at the wrong branch or folder, or deploys from Actions. Redo the Pages part of step 3, run `gh api -X POST repos/$R/pages/builds`, then `gh run rerun <id> -R $R` |
| `needs a repo named <owner>.github.io` | Rename the repo, or set `base-url` to a custom domain |
| `GITPIN_KEY secret is not set` / `not an Ed25519 key` / `has a passphrase` | Redo the key and secret from step 2 |
| `GITPIN_KEY belongs to ... but gitpin.json was written by ...` | The key changed. Delete `gitpin.json` and `ipni/` from `gh-pages`, then re-run |
| `redirects to ...` | Set `base-url` to the exact origin Pages serves |

Reference: https://github.com/ipni/gitpin. Its README covers custom domains, the local CLI, and limits (500 MiB and 50,000 blocks per repo).
