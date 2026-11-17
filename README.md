# gitpin

gitpin gives you a CID, a name for exact bytes that anyone can verify and that keeps working wherever the bytes are hosted. Point it at a folder in a GitHub repo, and you get a link that names your files exactly as they are: datasets you cite, NFT or dapp assets, software releases, ENS sites. If you only want a website, just use GitHub Pages.

Here a "pin" is something published from the `pins/` folder, not a copy kept on many machines. Your files are hosted by GitHub Pages, so they are online exactly while your Pages site is; the CID is the lasting part, and it keeps working if the files are later added to a pinning service or your own IPFS node.

## Quick start

1. On [github.com/ipni/gitpin-template](https://github.com/ipni/gitpin-template) click "Use this template" > "Create a new repository". Name it exactly `<your-username>.github.io` and make it **Public**.
2. Open the [keygen page](https://ipni.github.io/gitpin/keygen/), click "Make a key", copy the key.
3. In the new repo: Settings > Secrets and variables > Actions > New repository secret, name `GITPIN_KEY`, paste, Add secret.
4. Actions tab > "gitpin" workflow > "Run workflow". The first run creates the `gh-pages` branch, then waits up to 10 minutes for Pages and fails, because Pages is not on yet.
5. Settings > Pages > Build and deployment > Source "Deploy from a branch", Branch `gh-pages`, folder `/ (root)`, Save.
6. Re-run the workflow. When it is green, the run summary lists each pin with its CID and links; `https://<your-username>.github.io` shows the same list.

The [template README](https://github.com/ipni/gitpin-template#readme) walks through each step in detail.

## How it works

```
   your repo                 this repo's action                  GitHub Pages
 +------------+   push   +---------------------------+   push   +----------------+
 |  pins/     | --------> | UnixFS blocks (CIDv1,     | --------> | ipfs/<cid>     |
 |            |          | 256 KiB chunks)           |          | ipni/v1/ad/... |
 +------------+          | IPNI advertisements       |          | ipni/v1/ad/head|
                         | + signed head             |          +-------+--------+
                         | one commit to gh-pages    |                |
                         +---------------------------+                | HTTPS
                                                                      v
   IPFS client <---- blocks, verified against CIDs ----+   cid.contact (IPNI indexer)
        |                                               ^
        +---- asks for locations (delegated routing) ---+
```

The GitHub Action builds your files into UnixFS blocks (CIDv1, 256 KiB chunks) and writes each one to `ipfs/<cid>`; it writes IPNI advertisements to `ipni/v1/ad/` and a signed head to `ipni/v1/ad/head`; it pushes the result to the `gh-pages` branch as a single commit, which GitHub Pages serves over HTTPS. The action then announces the head to `https://cid.contact/announce`. cid.contact fetches the advertisements from your site and records that your site serves those CIDs over the trustless gateway protocol. IPFS clients ask cid.contact, directly or through delegated routing, fetch the blocks from your site, and check every block against its CID.

## Action reference

The workflow uses `actions/checkout@v4` with its default credentials, then the action. Do not set `persist-credentials: false` on `actions/checkout`; it breaks the push to `gh-pages`.

The template workflow, `template/.github/workflows/gitpin.yml`:

```yaml
name: gitpin
on:
  push:
    branches: [main]
    paths: ['pins/**']
  workflow_dispatch:
permissions:
  contents: write
concurrency:
  group: gitpin
  cancel-in-progress: false
jobs:
  pin:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: ipni/gitpin@v1
        with:
          key: ${{ secrets.GITPIN_KEY }}
```

Inputs:

| input | default | description |
| --- | --- | --- |
| `key` | required, no default | the key that signs the advertisements; `${{ secrets.GITPIN_KEY }}` |
| `content` | `pins` | a path relative to the repo root whose top-level files and folders are the pins |
| `branch` | `gh-pages` | the branch the site is pushed to |
| `base-url` | `https://<owner>.github.io` | where the site is served from; set to `https://your.domain` for a custom domain |
| `indexer` | `https://cid.contact` | the IPNI indexer to announce to |
| `wait-indexed` | `true` | wait until cid.contact lists every pin's CID with this site as a provider before the run finishes; on timeout the run ends with a warning, not a failure |

Outputs:

| output | description |
| --- | --- |
| `head` | the signed IPNI advertisement head the run published |
| `peer-id` | the peer ID of the key |

## Use a custom domain

The order matters: `gh-pages` does not exist until the first run, and Pages is not on by default for a repo not named `<username>.github.io`.

1. Set up DNS for the domain. It must serve over https, because `base-url` must be https.
2. Run the workflow once. It fails after its wait, which is fine; this run creates `gh-pages`.
3. In Settings > Pages set Source to branch `gh-pages`, folder `/ (root)`, and enter the custom domain. GitHub writes a `CNAME` file to `gh-pages`, which gitpin keeps.
4. Set `base-url: https://your.domain` in the workflow's `with:` and run it again.

Then the repo can have any name. The site must be at the domain root, not a path.

## Command line

For people who want to run it locally:

```sh
go install github.com/ipni/gitpin/cmd/gitpin@latest
```

This builds the site directory locally; nothing is pushed or served. It is for testing, and for hosting the directory yourself.

```sh
gitpin keygen
gitpin publish --content DIR --site DIR --base-url URL   # key in GITPIN_KEY
gitpin announce --site DIR --base-url URL [--indexer URL]
gitpin wait-indexed --site DIR [--indexer URL] [--timeout 15m]
```

`go install` needs Go. `@latest` works only once the `v1` tag exists.

## Limits

- Your pins are public. Anyone can fetch them, and the CIDs and your peer ID are listed on cid.contact. Do not pin secrets or personal data.
- This is not durable storage. Your files are online while your GitHub Pages site is. Delete the repo or turn off Pages and they are gone (other IPFS nodes may still hold copies they fetched).
- At most 500 MiB and 50,000 blocks of content per repo. GitHub Pages limits a site to 1 GB and about 100 GB of traffic a month, and its terms are for websites, not bulk file hosting.
- Project sites (`<username>.github.io/<repo>`) are not supported: IPFS clients need the site at a domain root.
- Names starting with `.` (such as `.gitkeep`) are skipped at every level.
- Symlinks make the run fail with `unsupported file type: <path>`; use real files.
- Changing a file gives it a new CID, and the old CID's blocks are removed from the site and its record dropped, so an old CID someone cited stops resolving from this site.

## Make it last longer

Add the CIDs to a pinning service, or run `ipfs pin add <cid>` on your own Kubo node. The links do not change.

## Troubleshooting

- **Pages is not serving the new head** (the message is "... is not serving the new head yet"): Pages is off, the source is not set to the `gh-pages` branch, the repo is private, or the repo name is not `<owner>.github.io`.
- **The GITPIN_KEY secret is not set**: add the secret under Settings > Secrets and variables > Actions.
- **GITPIN_KEY belongs to ... but gitpin.json was written by ...**: the key changed. Make a new key, replace the secret, and delete `gitpin.json` and the `ipni/` folder from the `gh-pages` branch before the next run; `ipfs/` can stay. On GitHub: open the `gh-pages` branch, open `gitpin.json`, "..." > "Delete file", commit; open the `ipni` folder, "..." > "Delete directory", commit; or do the same in a local clone and push. The new key gets a new peer ID, and the old peer's records expire on cid.contact over time.
- **needs a repo named `<owner>.github.io`**: rename the repo, or use a custom domain.
- **"not indexed yet" warning**: the run ends with a yellow warning, not a failure. Wait a few minutes and re-run the workflow; a new pin can take a few minutes, sometimes up to an hour, to show up on cid.contact.
