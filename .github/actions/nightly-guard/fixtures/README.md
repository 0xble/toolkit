# Recorded Actions Responses

`runs.json` and `jobs.json` project the operational fields from the successful scheduled run at https://github.com/0xble/agents/actions/runs/40532109886, captured on 2026-10-27 with:

```sh
gh api repos/0xble/agents/actions/workflows/nightly.yml/runs --method GET -f branch=main -f event=schedule -f status=success -f per_page=1
gh api repos/0xble/agents/actions/runs/40532109886/attempts/1/jobs --method GET -f per_page=100
```

Repository, actor, and link metadata are omitted. The field names and retained values are unmodified. The workflow-run object contains `run_started_at` and `updated_at`, but no `completed_at`. Job and step objects do contain `completed_at`.

The rollout workflows skip the `source` job (or reusable `nightly` job) intentionally while their aggregate job can succeed. Only a successful job with a successful full `./bin/ci nightly` execution step supplies execution evidence. Tests derive no-op and malformed responses from these fixtures, without live API access.
