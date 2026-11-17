# gitpin

**Publish files from a GitHub repo to IPFS. No node to run, no service to pay.**

Put a file in your repo's `pins/` folder and push. A GitHub Action turns it into IPFS content, serves it from your GitHub Pages site, and lists it on [cid.contact](https://cid.contact) so IPFS clients can find it. You get a CID: a name for those exact bytes that anyone can check, and that keeps working wherever the bytes are hosted later.

gitpin is for people who need a CID: datasets you cite in a paper, NFT or dapp assets, software releases, ENS sites. If you only want a website, plain GitHub Pages is simpler and you do not need this.

## Contents

- [Example: your first CID](#example-your-first-cid)
- [More examples](#more-examples)
- [Get started](#get-started)
- [Why gitpin](#why-gitpin)
- [How it works](#how-it-works)
- [Configuration](#configuration)
- [Use a custom domain](#use-a-custom-domain)
- [Command line](#command-line)
- [Limits](#limits)
- [Troubleshooting](#troubleshooting)
- [Contributing](#contributing)
- [License](#license)

## Example: your first CID

The template repo ships one file:

```
pins/hello.txt    Hello from my first IPFS pin!
```

After setup (below, about five minutes), every push that changes `pins/` runs the action. The run summary lists what was published:

| Name | CID | Links |
| --- | --- | --- |
| hello.txt | `bafkreidq7firpceoqbimkvlqp54nguhgufy7ynnsfpl3ya2as7lmwn2tua` | open, ipfs.io, dweb.link, index |

That CID is the name of those 30 bytes, and it is the same on every machine in the world. Anyone can now fetch it, and their client checks the bytes against the CID:

```sh
# in a browser
https://inbrowser.link/ipfs/bafkreidq7firpceoqbimkvlqp54nguhgufy7ynnsfpl3ya2as7lmwn2tua

# with Kubo, the IPFS command-line node
ipfs cat bafkreidq7firpceoqbimkvlqp54nguhgufy7ynnsfpl3ya2as7lmwn2tua
Hello from my first IPFS pin!
```

Your site, `https://<your-username>.github.io`, shows the same list with the same links.

## More examples

**Publish a dataset folder.** Every top-level file or folder in `pins/` is one pin with its own CID. A folder gets a single directory CID, and each file inside is reachable by path:

```
pins/
  survey-2026/
    responses.csv
    codebook.pdf
```

```sh
ipfs cat <cid>/responses.csv
https://inbrowser.link/ipfs/<cid>/codebook.pdf
```

Cite `ipfs://<cid>` in your paper: a reader who gets bytes for that CID from anywhere knows they are the bytes you published.

**Fetch from a script.** Small files (up to 256 KiB) are one raw block, served as-is by your own site, and you can check them yourself:

```sh
curl -s https://<your-username>.github.io/ipfs/<cid> -o hello.txt
```

For anything bigger, or to have the check done for you, use Kubo (`ipfs cat <cid>`, version 0.43 or newer was tested) or an IPFS library such as Helia. The public gateways ipfs.io and dweb.link work in a web browser only; they answer scripts with an error.

**Change or remove a file.** Edit a file in `pins/` and push: it gets a new CID, and the old CID stops resolving from your site. Delete it from `pins/` and push: its blocks are removed from the site and cid.contact is told to drop the record.

**Keep a second copy.** Your site is the only server holding the files. To add another, pin the same CID elsewhere; the links do not change:

```sh
ipfs pin add <cid>     # on your own Kubo node
```

or add the CID to any IPFS pinning service.

## Get started

You need a GitHub account. Nothing to install.

1. On [github.com/ipni/gitpin-template](https://github.com/ipni/gitpin-template) click **Use this template** > **Create a new repository**. Name it exactly `<your-username>.github.io` and make it **Public** (free accounts need a public repo for Pages).
2. Open the [keygen page](https://ipni.github.io/gitpin/keygen/), click **Make a key**, and copy the key. It is made in your browser and never sent anywhere.
3. In the new repo: **Settings** > **Secrets and variables** > **Actions** > **New repository secret**. Name it `GITPIN_KEY`, paste the key, and click **Add secret**.
4. **Actions** tab > **gitpin** workflow > **Run workflow**. This first run creates the `gh-pages` branch, then waits up to 10 minutes for Pages and fails, because Pages is not serving that branch yet. That is expected.
5. **Settings** > **Pages** > **Build and deployment**: Source **Deploy from a branch**, Branch `gh-pages`, folder `/ (root)`, **Save**. GitHub usually has Pages on already for a repo named `<your-username>.github.io`, serving `main`; you only change the branch.
6. Re-run the workflow. When it is green, the run summary lists each pin with its CID and links.

The [template README](https://github.com/ipni/gitpin-template#readme) walks through the same steps for people new to GitHub Actions. Already have a `<your-username>.github.io` repo? See [Use a custom domain](#use-a-custom-domain).

## Why gitpin

**Why a CID and not just a URL?** A URL names a place; whoever controls the place decides what you get. A CID names the content itself: it is a hash, so a client that asks for a CID can tell if it got anything else. The same CID works whether the bytes come from your site, a pinning service, a colleague's laptop or a public gateway. That is what makes it useful for citations, releases and on-chain references.

**Why is publishing to IPFS usually hard?** To make a CID fetchable you need a machine that stays online, runs an IPFS node, and tells the network what it holds. Most people who need a CID do not want to run a server, and pinning services need an account and often a card.

**What gitpin does instead.** IPFS clients can fetch content over plain HTTPS from any server that follows the [trustless gateway](https://specs.ipfs.tech/http-gateways/trustless-gateway/) format, and they find those servers through [IPNI](https://docs.ipfs.tech/concepts/ipni/) indexers such as cid.contact. Both only need static files. So the action writes your content and its index records as static files, GitHub Pages serves them, and cid.contact reads them from your site. Nothing of ours stores your bytes.

**What "pin" means here.** In IPFS, pinning usually means keeping copies on many machines. Here a pin is just a file or folder in `pins/` that gitpin publishes. Your files are online exactly while your Pages site is. The CID is the part that lasts: add it to a pinning service or your own node any time, and every existing link keeps working.

## How it works

```
   your repo                 the gitpin action                   GitHub Pages
 +------------+   push   +---------------------------+   push   +-----------------+
 |  pins/     | -------> | UnixFS blocks (CIDv1,     | -------> | ipfs/<cid>      |
 |            |          | 256 KiB chunks)           |          | ipni/v1/ad/...  |
 +------------+          | IPNI advertisements       |          | ipni/v1/ad/head |
                         | + signed head             |          +--------+--------+
                         | one commit to gh-pages    |                   |
                         +---------------------------+                   | HTTPS
                                                                         v
   IPFS client <----- blocks, verified against CIDs -----+   cid.contact (IPNI indexer)
        |                                                 ^
        +----- asks where a CID is (delegated routing) ---+
```

On each run the action:

1. Builds every file and folder in `pins/` into UnixFS blocks (CIDv1, raw leaves, 256 KiB chunks) and writes each block to `ipfs/<cid>` on the site.
2. Writes IPNI advertisements to `ipni/v1/ad/`: one per new pin, a removal for each deleted pin, each signed with your key and linked to the previous one. It then writes a signed pointer to the newest one at `ipni/v1/ad/head`.
3. Pushes the whole site to the `gh-pages` branch as a single commit with no history, so removed blocks do not pile up.
4. Waits until GitHub Pages serves the new head, then announces it to `https://cid.contact/announce`.
5. cid.contact fetches the new advertisements from your site, checks the signatures, and records that your site serves those CIDs over the trustless gateway protocol.
6. Optionally waits until cid.contact lists every pin.

A client asks cid.contact (directly, or through delegated routing at delegated-ipfs.dev) where a CID is, gets your site's address, fetches the blocks over HTTPS and checks each one against its CID.

The published site looks like this:

```
.nojekyll          turns off Jekyll
index.html         the list of pins and links
gitpin.json        what is published (state for the next run)
ipfs/bafkqaaa      empty probe file IPFS clients request first
ipfs/<cid>         one file per block
ipni/v1/ad/head    signed pointer to the newest advertisement
ipni/v1/ad/<cid>   advertisements and entry lists
```

## Configuration

The template's workflow, `.github/workflows/gitpin.yml`:

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

Keep `actions/checkout` with its default credentials: `persist-credentials: false` breaks the push to `gh-pages`.

Inputs of `ipni/gitpin@v1`:

| Input | Default | Description |
| --- | --- | --- |
| `key` | required | The signing key: `${{ secrets.GITPIN_KEY }}`. |
| `content` | `pins` | Folder to publish, relative to the repo root. |
| `branch` | `gh-pages` | Branch the site is pushed to. |
| `base-url` | `https://<owner>.github.io` | Where the site is served. Set `https://your.domain` for a custom domain. |
| `indexer` | `https://cid.contact` | IPNI indexer to announce to. |
| `wait-indexed` | `true` | Wait until the indexer lists every pin. On timeout the run ends with a warning, not a failure. |

Outputs: `head` (CID of the newest advertisement) and `peer-id` (the peer ID of your key, which identifies your site on cid.contact).

## Use a custom domain

With a custom domain the repo can have any name, and an account that already uses `<your-username>.github.io` for something else can still use gitpin. The site must be at the domain root, not under a path.

1. Point the domain at GitHub Pages (see GitHub's custom domain docs). It must serve HTTPS.
2. Run the workflow once. It fails after its wait; this run creates `gh-pages`.
3. In **Settings** > **Pages**, set the source to branch `gh-pages`, folder `/ (root)`, and enter the domain. GitHub writes a `CNAME` file to `gh-pages`, which gitpin keeps.
4. Add `base-url: https://your.domain` under `with:` in the workflow and run it again.

## Command line

The action runs a small Go program you can also run yourself, to test or to host the site directory somewhere else. It builds the site locally; it does not push or serve anything.

```sh
go install github.com/ipni/gitpin/cmd/gitpin@latest

gitpin keygen                                             # prints GITPIN_KEY=... and PEER_ID=...
GITPIN_KEY=... gitpin publish --content pins --site site --base-url https://you.example
gitpin announce --site site --base-url https://you.example [--indexer URL]
gitpin wait-indexed --site site [--indexer URL] [--timeout 15m]
```

`publish` prints one JSON line: `{"changed":true,"head":"...","peer_id":"...","roots":[{"name":"hello.txt","cid":"..."}]}`. Exit codes: 0 ok, 1 error, 2 `wait-indexed` timed out (the files are published; the index is slow), 64 usage.

## Limits

- **Public.** Anyone can fetch your pins, and the CIDs and your peer ID are listed on cid.contact. Do not publish secrets or personal data.
- **Not durable storage.** Your files are online while your GitHub Pages site is. Delete the repo or turn off Pages and they are gone, except for copies other IPFS nodes already fetched.
- **Size.** At most 500 MiB and 50,000 blocks of content per repo. GitHub Pages limits a site to 1 GB and about 100 GB of traffic a month, and its terms are for websites, not bulk file hosting.
- **Root sites only.** Project sites (`<username>.github.io/<repo>`) are not supported: IPFS clients need the site at a domain root.
- **Files only.** Names starting with `.` are skipped. Symlinks fail the run with `unsupported file type: <path>`.
- **Indexing takes time.** A new pin usually shows up on cid.contact within minutes, sometimes up to an hour. Until then clients may not find it.
- **Your key.** It signs your advertisements. Keep it only in the `GITPIN_KEY` secret.

## Troubleshooting

- **"... is not serving the new head yet"**: Pages is off, its source is not the `gh-pages` branch, the repo is private, or the repo is not named `<owner>.github.io`. Fix the setting and re-run the workflow.
- **"The GITPIN_KEY secret is not set"**: add the secret (step 3 of [Get started](#get-started)).
- **"needs a repo named `<owner>.github.io`"**: rename the repo, or [use a custom domain](#use-a-custom-domain).
- **"GITPIN_KEY belongs to ... but gitpin.json was written by ..."**: the key changed since the last run. If that was on purpose (for example after a leak), delete `gitpin.json` and the `ipni/` folder from the `gh-pages` branch, then re-run; `ipfs/` can stay. The new key has a new peer ID; records under the old one expire on cid.contact over time.
- **Yellow "not indexed yet" warning**: your files are published. Wait a few minutes and re-run the workflow, or just check the index link later.

## Contributing

The repo holds the Go program (`cmd/gitpin`, `internal/`), the composite action (`action.yml`), the template repo's files (`template/`) and the key page (`keygen/`).

```sh
go test ./...                              # Go packages
node --test keygen/keygen.test.mjs         # key page (Node 24)
```

Issues and pull requests are welcome at [github.com/ipni/gitpin](https://github.com/ipni/gitpin).

## License

Dual-licensed under [Apache-2.0](https://www.apache.org/licenses/LICENSE-2.0) or [MIT](https://opensource.org/licenses/MIT), at your option, like other IPNI and IPFS projects. See [LICENSE.md](LICENSE.md).
