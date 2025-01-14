from __future__ import annotations

import inspect
import sys
from pathlib import Path
from typing import Callable, Dict


sys.path.append(str(Path(__file__).parent / "fixtures/lambdas"))

Params = Dict[str, inspect.Parameter]


class Context:
    aws_request_id: str

    def get_remaining_time_in_millis(self):
        return 100


context = Context()
context.aws_request_id = "test-request"

TEST_ORG = "test-org"
TEST_FUNCTION = "test-function"
TEST_FUNCTION_VERSION = "1"
TEST_DEV_MODE_ORG_ID = "test-dev-mode-org-id"


def get_params(func: Callable) -> Params:
    signature = inspect.signature(func)

    return signature.parameters


def compare_handlers(original, instrumented):
    assert callable(original) and callable(instrumented)

    orig_params = get_params(original)
    instrumented_params = get_params(instrumented)
    assert orig_params == instrumented_params

    orig_result = original({}, context)
    instrumented_result = instrumented({}, context)
    assert orig_result == instrumented_result
