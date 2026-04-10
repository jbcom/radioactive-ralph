from click.testing import CliRunner
from radioactive_ralph.cli import main, run, status, doctor

def test_main_cli():
    runner = CliRunner()
    result = runner.invoke(main, ["--help"])
    assert result.exit_code == 0
    assert "Radioactive Ralph" in result.output

def test_main_verbose(mocker):
    mocker.patch("radioactive_ralph.cli.logging.basicConfig")
    runner = CliRunner()
    result = runner.invoke(main, ["--verbose", "status"])
    pass

def test_run_command(mocker):
    mock_load_config = mocker.patch("radioactive_ralph.cli.load_config")
    mock_orchestrator = mocker.patch("radioactive_ralph.cli.Orchestrator")
    mock_asyncio_run = mocker.patch("radioactive_ralph.cli.asyncio.run")
    
    runner = CliRunner()
    result = runner.invoke(run, ["--variant", "joe-fixit"])
    
    assert result.exit_code == 0
    mock_load_config.assert_called_once()
    mock_orchestrator.assert_called_once()
    mock_asyncio_run.assert_called_once()

def test_run_command_keyboard_interrupt(mocker):
    mock_load_config = mocker.patch("radioactive_ralph.cli.load_config")
    mock_orchestrator = mocker.patch("radioactive_ralph.cli.Orchestrator")
    mock_instance = mock_orchestrator.return_value
    mock_asyncio_run = mocker.patch("radioactive_ralph.cli.asyncio.run", side_effect=KeyboardInterrupt)
    
    runner = CliRunner()
    result = runner.invoke(run, ["--variant", "savage"])
    
    assert result.exit_code == 0
    mock_instance.stop.assert_called_once()

def test_status_command(mocker):
    mock_load_config = mocker.patch("radioactive_ralph.cli.load_config")
    mock_load_state = mocker.patch("radioactive_ralph.state.load_state")
    mock_render = mocker.patch("radioactive_ralph.cli.render_dashboard")
    
    runner = CliRunner()
    result = runner.invoke(status)
    
    assert result.exit_code == 0
    mock_render.assert_called_once()

def test_doctor_command(mocker):
    mock_load_config = mocker.patch("radioactive_ralph.cli.load_config")
    mock_check_health = mocker.patch("radioactive_ralph.cli.check_health")
    
    runner = CliRunner()
    result = runner.invoke(doctor)
    
    assert result.exit_code == 0
    mock_check_health.assert_called_once()
