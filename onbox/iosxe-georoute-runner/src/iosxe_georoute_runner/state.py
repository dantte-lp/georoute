from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from pathlib import Path


@dataclass(frozen=True, slots=True)
class CandidateState:
    candidate: str
    digest: str
    generated_at: datetime
    stale_after: timedelta

    def ready(self, now: datetime | None = None) -> bool:
        if not self.candidate or not self.digest:
            return False
        current_time = now or datetime.now(UTC)
        return current_time - self.generated_at <= self.stale_after


def write_last_success_marker(path: Path, state: CandidateState) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        "\n".join(
            [
                f"generated_at={state.generated_at.isoformat()}",
                f"sha256={state.digest}",
            ]
        )
        + "\n",
        encoding="utf-8",
    )
