from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class ApplyPlan:
    candidate_url: str
    candidate_sha256: str


def build_copy_command(plan: ApplyPlan) -> str:
    return f"copy {plan.candidate_url} running-config"
