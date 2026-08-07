"""A very small GitHub REST client.

`gh` is present on the hosted runners, but shelling out to it makes the script
awkward to test and hides the failure modes. Standard-library HTTP keeps the
dependency surface at exactly one package (PyYAML) and makes every request
visible.
"""

from __future__ import annotations

import json
import os
import time
import urllib.error
import urllib.parse
import urllib.request
from typing import Any

API = "https://api.github.com"


class GitHubError(RuntimeError):
    def __init__(self, status: int, message: str, url: str):
        super().__init__(f"{status} {message} ({url})")
        self.status = status
        self.url = url


class GitHub:
    def __init__(self, token: str, repo: str, *, dry_run: bool = False):
        if not token:
            raise ValueError("a token is required")
        if "/" not in repo:
            raise ValueError(f"repo must be owner/name, got {repo!r}")
        self.token = token
        self.repo = repo
        self.dry_run = dry_run

    # ------------------------------------------------------------------ core
    def request(
        self,
        method: str,
        path: str,
        body: Any = None,
        *,
        accept: str = "application/vnd.github+json",
        retries: int = 3,
    ) -> Any:
        url = path if path.startswith("http") else f"{API}{path}"
        data = json.dumps(body).encode() if body is not None else None
        request = urllib.request.Request(url, data=data, method=method)
        request.add_header("Authorization", f"Bearer {self.token}")
        request.add_header("Accept", accept)
        request.add_header("X-GitHub-Api-Version", "2022-11-28")
        request.add_header("User-Agent", "switchmtp-triage")
        if data is not None:
            request.add_header("Content-Type", "application/json")

        last: Exception | None = None
        for attempt in range(retries):
            try:
                with urllib.request.urlopen(request, timeout=30) as response:
                    raw = response.read()
                    return json.loads(raw) if raw else None
            except urllib.error.HTTPError as exc:
                detail = exc.read().decode("utf-8", "replace")[:400]
                # Secondary rate limits and 5xx are worth another go; 4xx is not.
                if exc.code in (403, 429) and "rate limit" in detail.lower():
                    time.sleep(2 ** attempt * 5)
                    last = GitHubError(exc.code, detail, url)
                    continue
                if 500 <= exc.code < 600:
                    time.sleep(2 ** attempt)
                    last = GitHubError(exc.code, detail, url)
                    continue
                raise GitHubError(exc.code, detail, url) from exc
            except urllib.error.URLError as exc:
                last = exc
                time.sleep(2 ** attempt)
        raise last if last else RuntimeError("request failed")

    def paginate(self, path: str, **params: Any) -> list[Any]:
        params.setdefault("per_page", 100)
        out: list[Any] = []
        page = 1
        while True:
            query = urllib.parse.urlencode({**params, "page": page})
            batch = self.request("GET", f"{path}?{query}")
            if not batch:
                break
            out.extend(batch)
            if len(batch) < params["per_page"] or page >= 10:
                break
            page += 1
        return out

    # --------------------------------------------------------------- issues
    def issue(self, number: int) -> dict[str, Any]:
        return self.request("GET", f"/repos/{self.repo}/issues/{number}")

    def open_issues(self, *, since: str | None = None) -> list[dict[str, Any]]:
        params: dict[str, Any] = {"state": "open", "sort": "created", "direction": "desc"}
        if since:
            params["since"] = since
        issues = self.paginate(f"/repos/{self.repo}/issues", **params)
        return [i for i in issues if "pull_request" not in i]

    def comments(self, number: int) -> list[dict[str, Any]]:
        return self.paginate(f"/repos/{self.repo}/issues/{number}/comments")

    def create_comment(self, number: int, body: str) -> dict[str, Any] | None:
        if self.dry_run:
            print(f"[dry-run] would comment on #{number}")
            return None
        return self.request(
            "POST", f"/repos/{self.repo}/issues/{number}/comments", {"body": body}
        )

    def update_comment(self, comment_id: int, body: str) -> dict[str, Any] | None:
        if self.dry_run:
            print(f"[dry-run] would edit comment {comment_id}")
            return None
        return self.request(
            "PATCH", f"/repos/{self.repo}/issues/comments/{comment_id}", {"body": body}
        )

    def add_labels(self, number: int, labels: list[str]) -> None:
        if not labels:
            return
        if self.dry_run:
            print(f"[dry-run] would add {labels} to #{number}")
            return
        self.request(
            "POST", f"/repos/{self.repo}/issues/{number}/labels", {"labels": labels}
        )

    def remove_label(self, number: int, label: str) -> None:
        if self.dry_run:
            print(f"[dry-run] would remove {label!r} from #{number}")
            return
        try:
            self.request(
                "DELETE",
                f"/repos/{self.repo}/issues/{number}/labels/"
                f"{urllib.parse.quote(label, safe='')}",
            )
        except GitHubError as exc:
            if exc.status != 404:
                raise

    def close_issue(self, number: int, reason: str = "completed") -> None:
        if self.dry_run:
            print(f"[dry-run] would close #{number} as {reason}")
            return
        self.request(
            "PATCH",
            f"/repos/{self.repo}/issues/{number}",
            {"state": "closed", "state_reason": reason},
        )

    def latest_release_tag(self) -> str:
        try:
            release = self.request("GET", f"/repos/{self.repo}/releases/latest")
        except GitHubError as exc:
            if exc.status == 404:
                return ""
            raise
        return str((release or {}).get("tag_name", ""))

    # -------------------------------------------------------------- copilot
    def copilot_can_be_assigned(self) -> bool:
        """Is the Copilot coding agent available as an assignee on this repo?"""
        owner, name = self.repo.split("/", 1)
        query = """
        query($owner: String!, $name: String!) {
          repository(owner: $owner, name: $name) {
            suggestedActors(capabilities: [CAN_BE_ASSIGNED], first: 100) {
              nodes { login __typename }
            }
          }
        }
        """
        try:
            result = self.request(
                "POST",
                f"{API}/graphql",
                {"query": query, "variables": {"owner": owner, "name": name}},
            )
        except GitHubError:
            return False
        nodes = (
            ((result or {}).get("data") or {}).get("repository", {})
            or {}
        ).get("suggestedActors", {}).get("nodes") or []
        return any(n.get("login") == "copilot-swe-agent" for n in nodes)

    def assign_copilot(self, number: int, instructions: str = "") -> bool:
        """Hand an issue to the Copilot coding agent.

        Requires a user-to-server token; the workflow's GITHUB_TOKEN will not
        do. Returns False rather than raising when the delegation is refused,
        so a missing or under-scoped token degrades to a no-op.
        """
        if self.dry_run:
            print(f"[dry-run] would delegate #{number} to the Copilot coding agent")
            return True
        payload: dict[str, Any] = {"assignees": ["copilot-swe-agent"]}
        if instructions:
            payload["agent_assignment"] = {"custom_instructions": instructions}
        try:
            self.request(
                "POST", f"/repos/{self.repo}/issues/{number}/assignees", payload
            )
            return True
        except GitHubError as exc:
            print(f"::warning::could not delegate #{number} to Copilot: {exc}")
            return False


def token_from_env() -> str:
    for name in ("TRIAGE_TOKEN", "GITHUB_TOKEN", "GH_TOKEN"):
        value = os.environ.get(name)
        if value:
            return value
    return ""
