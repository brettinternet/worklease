"""Example external source-provider package for the SDK compatibility guide."""

from .policy import ExamplePolicy, registration
from .provider import ExampleProvider

__all__ = ["ExamplePolicy", "ExampleProvider", "registration"]
