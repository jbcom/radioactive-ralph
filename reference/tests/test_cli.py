"""Tests for the CLI.

`run` is stubbed during the M1→M2 rewrite and exits 2 with a PRD pointer.
`status` and `doctor` remain fully implemented.
"""

from click.testing import CliRunner

from radioactive_ralph.cli import doctor, main, run, status


def test_main_cli() -> None:
    runner = CliRunner()
    result = runner.invoke(main, ["--help"])
    assert result.exit_code == 0
    assert "Radioactive Ralph" in result.output


def test_main_verbose(mocker) -> None:
    """`--verbose` sets DEBUG log level before dispatching to a subcommand."""
    basic_config = mocker.patch("radioactive_ralph.cli.logging.basicConfig")
    mocker.patch("radioactive_ralph.cli.load_config")
    mocker.patch("radioactive_ralph.state.load_state")
    mocker.patch("radioactive_ralph.cli.render_dashboard")

    runner = CliRunner()
    result = runner.invoke(main, ["--verbose", "status"])

    assert result.exit_code == 0
    basic_config.assert_called_once()
    # first positional arg to basicConfig is `level`; matches logging.DEBUG (10)
    assert basic_config.call_args.kwargs["level"] == 10


def test_run_command_is_stubbed() -> None:
    """`ralph run` exits 2 with the rewrite pointer during M1."""
    runner = CliRunner()
    result = runner.invoke(run, ["--variant", "joe-fixit"])

    assert result.exit_code == 2
    assert "under rewrite" in result.output
    assert "docs/plans/2026-04-14" in result.output


def test_status_command(mocker) -> None:
    mocker.patch("radioactive_ralph.cli.load_config")
    mocker.patch("radioactive_ralph.state.load_state")
    mock_render = mocker.patch("radioactive_ralph.cli.render_dashboard")

    runner = CliRunner()
    result = runner.invoke(status)

    assert result.exit_code == 0
    mock_render.assert_called_once()


def test_doctor_command(mocker) -> None:
    mocker.patch("radioactive_ralph.cli.load_config")
    mock_check_health = mocker.patch("radioactive_ralph.cli.check_health")

    runner = CliRunner()
    result = runner.invoke(doctor)

    assert result.exit_code == 0
    mock_check_health.assert_called_once()
