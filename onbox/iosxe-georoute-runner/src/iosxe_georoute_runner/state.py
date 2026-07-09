from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta
from pathlib import Path


@dataclass(slots=True)
class CandidateState:
    candidate: str = ""
    digest: str = ""
    generated_at: datetime = field(default_factory=lambda: datetime.fromtimestamp(0, UTC))
    stale_after: timedelta = field(default_factory=lambda: timedelta(minutes=30))
    last_error: str | None = None

    @classmethod
    def empty(cls, stale_after: timedelta | None = None) -> CandidateState:
        return cls(stale_after=stale_after or timedelta(minutes=30))

    def update(self, candidate: str, digest: str, generated_at: datetime | None = None) -> None:
        self.candidate = candidate
        self.digest = digest
        self.generated_at = generated_at or datetime.now(UTC)
        self.last_error = None

    def mark_error(self, exc: BaseException) -> None:
        self.last_error = f"{type(exc).__name__}: {exc}"

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
