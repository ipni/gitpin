# scripts

`test-action.sh` exercises the shell steps of the composite action in `action.yml`
(check, site, publish, push, wait-indexed, summary) locally against a throwaway bare
git repo, without touching GitHub or the network. It needs bash, git, go, jq and
python3 with PyYAML. Run it from the repo root: `bash scripts/test-action.sh`.
