from radioactive_ralph.config import RadioactiveRalphConfig
from radioactive_ralph.dashboard import render_dashboard
from radioactive_ralph.models import OrchestratorState, WorkItem, WorkPriority


def test_render_dashboard_empty_queue(mocker):
    mock_print = mocker.patch("radioactive_ralph.dashboard.console.print")
    state = OrchestratorState()
    config = RadioactiveRalphConfig()

    render_dashboard(state, config)
    mock_print.assert_called_once()


def test_render_dashboard_with_queue(mocker):
    mock_print = mocker.patch("radioactive_ralph.dashboard.console.print")
    state = OrchestratorState()
    state.work_queue.append(
        WorkItem(
            id="test_1",
            repo_name="test",
            repo_path=".",
            description="test",
            priority=WorkPriority.HIGH,
        )
    )
    config = RadioactiveRalphConfig()

    render_dashboard(state, config)
    assert mock_print.call_count == 2
