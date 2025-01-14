import pytest
from unittest.mock import MagicMock
import logging
import json


@pytest.fixture()
def instrumented_logging():
    import sls_sdk.lib.instrumentation.logging

    sls_sdk.lib.instrumentation.logging.install()
    yield sls_sdk.lib.instrumentation.logging
    sls_sdk.lib.instrumentation.logging.uninstall()


def test_instrument_error(instrumented_logging, monkeypatch):
    # given
    mock = MagicMock()
    monkeypatch.setattr(instrumented_logging, "create_error_captured_event", mock)
    error = Exception("My error")

    # when
    logging.error(error)

    # then
    mock.assert_called_once_with(error, origin="pythonLogging")


def test_instrument_error_with_multiple_arguments(instrumented_logging, monkeypatch):
    # given
    mock = MagicMock()
    monkeypatch.setattr(instrumented_logging, "create_error_captured_event", mock)
    error = "%s %s went wrong"
    args = ("logging", "test")

    # when
    logging.error(error, *args, exc_info=True)

    # then
    mock.assert_called_once_with(error % args, origin="pythonLogging")


def test_instrument_error_only_exception_as_argument(instrumented_logging, monkeypatch):
    # given
    mock = MagicMock()
    monkeypatch.setattr(
        instrumented_logging,
        "create_error_captured_event",
        mock,
    )
    ex = Exception("Something is wrong")

    # when
    logging.error(ex)

    # then
    mock.assert_called_once_with(ex, origin="pythonLogging")


def test_instrument_error_integer_argument(instrumented_logging, monkeypatch):
    # given
    mock = MagicMock()
    monkeypatch.setattr(
        instrumented_logging,
        "create_error_captured_event",
        mock,
    )

    # when
    logging.error(10)

    # then
    mock.assert_called_once_with("10", origin="pythonLogging")


def test_instrument_error_invalid_usage(instrumented_logging, monkeypatch):
    # given
    mock = MagicMock()
    monkeypatch.setattr(
        instrumented_logging,
        "create_error_captured_event",
        mock,
    )
    error = None

    # when
    try:
        logging.error(10, 10)
    except TypeError as ex:
        error = ex

    # then
    assert error
    mock.assert_not_called()


def test_instrument_warning(instrumented_logging, monkeypatch):
    # given
    mock = MagicMock()
    monkeypatch.setattr(
        instrumented_logging,
        "create_warning_captured_event",
        mock,
    )
    message = "My message: %s"

    # when
    logging.warning(message, "hello")

    # then
    mock.assert_called_once_with(message % "hello", origin="pythonLogging")


def test_instrument_warn(instrumented_logging, monkeypatch):
    # given
    mock = MagicMock()
    monkeypatch.setattr(
        instrumented_logging,
        "create_warning_captured_event",
        mock,
    )
    message = "My message: %s"

    # when
    logging.warn(message, "hello")

    # then
    mock.assert_called_once_with(message % "hello", origin="pythonLogging")


def test_instrument_warning_recognize_sdk_warning(instrumented_logging, monkeypatch):
    # given
    mock = MagicMock()
    monkeypatch.setattr(
        instrumented_logging,
        "create_warning_captured_event",
        mock,
    )
    mock_json = MagicMock()
    monkeypatch.setattr(json, "dumps", mock_json)
    message = "Something is wrong"

    # when
    data = {"source": "serverlessSdk", "message": message}
    logging.warning(data)

    # then
    mock.assert_not_called()
    mock_json.assert_called_once_with(data, indent=2)


def test_instrument_warning_recognize_sdk_error(instrumented_logging, monkeypatch):
    # given
    mock = MagicMock()
    monkeypatch.setattr(
        instrumented_logging,
        "create_error_captured_event",
        mock,
    )
    mock_json = MagicMock()
    monkeypatch.setattr(json, "dumps", mock_json)
    message = "Something is wrong"

    # when
    data = {"source": "serverlessSdk", "message": message}
    logging.error(data)

    # then
    mock.assert_not_called()
    mock_json.assert_called_once_with(data, indent=2)


def test_instrument_warning_only_exception_as_argument(
    instrumented_logging, monkeypatch
):
    # given
    mock = MagicMock()
    monkeypatch.setattr(
        instrumented_logging,
        "create_warning_captured_event",
        mock,
    )
    ex = Exception("Something is wrong")

    # when
    logging.warning(ex)

    # then
    mock.assert_called_once_with(ex, origin="pythonLogging")


def test_instrument_warning_integer_argument(instrumented_logging, monkeypatch):
    # given
    mock = MagicMock()
    monkeypatch.setattr(
        instrumented_logging,
        "create_warning_captured_event",
        mock,
    )

    # when
    logging.warning(10)

    # then
    mock.assert_called_once_with("10", origin="pythonLogging")


def test_instrument_warning_invalid_usage(instrumented_logging, monkeypatch):
    # given
    mock = MagicMock()
    monkeypatch.setattr(
        instrumented_logging,
        "create_warning_captured_event",
        mock,
    )
    error = None

    # when
    try:
        logging.warning(10, 10)
    except TypeError as ex:
        error = ex

    # then
    assert error
    mock.assert_not_called()
