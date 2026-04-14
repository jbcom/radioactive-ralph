from __future__ import annotations

import contextlib
from collections.abc import Generator
from enum import Enum
from typing import Any

class Variant(Enum):
    SAVAGE: str
    JOE_FIXIT: str
    OLD_MAN: str

def ralph_says(variant: Variant, key: str, **kwargs: Any) -> None: ...
@contextlib.contextmanager
def ralph_panel(variant: Variant, title: str) -> Generator[None, None, None]: ...
