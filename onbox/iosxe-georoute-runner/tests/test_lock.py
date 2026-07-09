from pathlib import Path

import pytest

from iosxe_georoute_runner.lock import RefreshLock, RefreshLockError
from iosxe_georoute_runner.state import CandidateState, write_last_success_marker


def test_refresh_lock_rejects_concurrent_acquire(tmp_path: Path) -> None:
    lock_path = tmp_path / "refresh.lock"

    with RefreshLock(lock_path), pytest.raises(RefreshLockError):
        RefreshLock(lock_path).acquire()

    assert not lock_path.exists()


def test_last_success_marker_is_written(tmp_path: Path, ready_state: CandidateState) -> None:
    marker = tmp_path / "last-success"

    write_last_success_marker(marker, ready_state)

    body = marker.read_text(encoding="utf-8")
    assert "sha256=abc123" in body


@pytest.fixture
def ready_state() -> CandidateState:
    from datetime import UTC, datetime, timedelta

    return CandidateState(
        candidate="candidate\n",
        digest="abc123",
        generated_at=datetime.now(UTC),
        stale_after=timedelta(minutes=5),
    )
