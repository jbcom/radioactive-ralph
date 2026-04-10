import logging
from radioactive_ralph.logging_setup import setup_logging

def test_setup_logging(mocker):
    mock_install = mocker.patch("radioactive_ralph.logging_setup.install_rich_tracebacks")
    mock_basic_config = mocker.patch("radioactive_ralph.logging_setup.logging.basicConfig")
    
    setup_logging(logging.DEBUG)
    
    mock_install.assert_called_once_with(show_locals=True, max_frames=5)
    mock_basic_config.assert_called_once()
    assert mock_basic_config.call_args[1]["level"] == logging.DEBUG
    
    # Check that library loggers are silenced
    assert logging.getLogger("httpx").level == logging.WARNING
    assert logging.getLogger("httpcore").level == logging.WARNING
    assert logging.getLogger("anthropic").level == logging.WARNING
