"""Bounded local cache, with resource cleanup delegated to the context manager."""
from contextlib import contextmanager


@contextmanager
def open_cache(path):
    handle = open(path, "r", encoding="utf-8")
    try:
        yield handle
    finally:
        handle.close()


def load_value(path):
    with open_cache(path) as handle:
        return handle.read().strip()
