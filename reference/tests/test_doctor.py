import os

from radioactive_ralph.config import RadioactiveRalphConfig
from radioactive_ralph.doctor import check_health


def test_check_health_all_pass(mocker):
    config = RadioactiveRalphConfig()
    mocker.patch("radioactive_ralph.doctor.shutil.which", return_value="/usr/bin/mock")
    mocker.patch.dict(os.environ, {"ANTHROPIC_API_KEY": "fake_key"})
    mock_print = mocker.patch("radioactive_ralph.doctor.console.print")

    assert check_health(config) is True
    mock_print.assert_called_once()


def test_check_health_missing_claude(mocker):
    config = RadioactiveRalphConfig()

    def mock_which(cmd):
        if cmd == "claude":
            return None
        return "/usr/bin/mock"

    mocker.patch("radioactive_ralph.doctor.shutil.which", side_effect=mock_which)
    mocker.patch.dict(os.environ, {"ANTHROPIC_API_KEY": "fake_key"})

    assert check_health(config) is False


def test_check_health_missing_key(mocker):
    config = RadioactiveRalphConfig()
    mocker.patch("radioactive_ralph.doctor.shutil.which", return_value="/usr/bin/mock")

    # We remove it if it exists
    if "ANTHROPIC_API_KEY" in os.environ:
        mocker.patch.dict(os.environ, clear=False)
        del os.environ["ANTHROPIC_API_KEY"]

    assert check_health(config) is False


def test_check_health_missing_gh(mocker):
    config = RadioactiveRalphConfig()

    def mock_which(cmd):
        if cmd == "gh":
            return None
        return "/usr/bin/mock"

    mocker.patch("radioactive_ralph.doctor.shutil.which", side_effect=mock_which)
    mocker.patch.dict(os.environ, {"ANTHROPIC_API_KEY": "fake_key"})
    mocker.patch("radioactive_ralph.doctor.console.print")

    assert check_health(config) is True
    # The warning should be in the table
