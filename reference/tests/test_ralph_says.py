from rich.panel import Panel

from radioactive_ralph.ralph_says import _RECENT_EVENTS, Variant, ralph_panel, ralph_says


def test_ralph_says(mocker):
    mock_print = mocker.patch("radioactive_ralph.ralph_says.console.print")

    initial_len = len(_RECENT_EVENTS)
    ralph_says(Variant.SAVAGE, "startup")

    mock_print.assert_called_once()
    assert "I'm awake. Time to break things." in mock_print.call_args[0][0]
    assert len(_RECENT_EVENTS) == initial_len + 1

    ralph_says(Variant.SAVAGE, "unknown_key")
    assert "Unknown message: unknown_key" in mock_print.call_args[0][0]

    ralph_says(Variant.SAVAGE, "merging", pr=123)
    assert "Squashing PR #123. It's for the best." in mock_print.call_args[0][0]


def test_ralph_panel(mocker):
    mock_print = mocker.patch("radioactive_ralph.ralph_says.console.print")

    with ralph_panel(Variant.JOE_FIXIT, "Test Task"):
        pass

    assert mock_print.call_count == 2
    # The first call argument is a Rich Panel object
    panel = mock_print.call_args_list[0][0][0]
    assert isinstance(panel, Panel)
    assert panel.renderable == "Starting Test Task..."

    assert "Finished Test Task" in str(mock_print.call_args_list[1])
