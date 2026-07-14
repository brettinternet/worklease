"""Versioned, lazy resource-policy registry.

Resource policies define deterministic resource identities and claim capabilities.
They deliberately do not discover providers, perform network calls, or mutate
provider state. Built-ins are described without importing their modules; an
adapter module is imported only after its policy is selected. External policies
are exposed through the ``worklease.resource_policies`` entry-point group and
must return a :class:`ResourcePolicyRegistration`.
"""

from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass
from importlib import import_module, metadata
from typing import Any, Final, cast

from ..models import LeaseError
from .protocol import ProviderAdapter, normalize_provider

RESOURCE_POLICY_CONTRACT_VERSION: Final = 1
RESOURCE_POLICY_ENTRY_POINT_GROUP: Final = "worklease.resource_policies"

PolicyFactory = Callable[[str], ProviderAdapter]


@dataclass(frozen=True, slots=True)
class ResourcePolicyDescriptor:
    """Validated metadata for one resource-key policy."""

    name: str
    origin: str
    origin_version: str
    contract_version: int = RESOURCE_POLICY_CONTRACT_VERSION
    key_policy_version: int = 1
    scope: str = "item"
    capability: str = "local-coordination"
    generic_execution_guarantee: str = "local-coordination"
    provider_fencing_supported: bool = False

    def to_dict(self) -> dict[str, object]:
        """Return stable inspection metadata without loading the policy."""

        return {
            "name": self.name,
            "origin": self.origin,
            "originVersion": self.origin_version,
            "contractVersion": self.contract_version,
            "keyPolicyVersion": self.key_policy_version,
            "scope": self.scope,
            "capability": self.capability,
            "genericExecutionGuarantee": self.generic_execution_guarantee,
            "providerFencingSupported": self.provider_fencing_supported,
        }


@dataclass(frozen=True, slots=True)
class ResourcePolicyRegistration:
    """Entry-point payload for one versioned resource policy."""

    descriptor: ResourcePolicyDescriptor
    factory: PolicyFactory


@dataclass(frozen=True, slots=True)
class _BuiltinSpec:
    descriptor: ResourcePolicyDescriptor
    module: str


_BUILTINS: Final[dict[str, _BuiltinSpec]] = {
    "backlog-md": _BuiltinSpec(
        ResourcePolicyDescriptor(
            name="backlog-md",
            origin="worklease",
            origin_version="0.1.0",
            scope="item",
            capability="item-claim",
        ),
        ".backlog_md",
    ),
    "github": _BuiltinSpec(
        ResourcePolicyDescriptor(
            name="github",
            origin="worklease",
            origin_version="0.1.0",
            scope="item",
            capability="item-claim",
        ),
        ".github",
    ),
    "generic": _BuiltinSpec(
        ResourcePolicyDescriptor(
            name="generic",
            origin="worklease",
            origin_version="0.1.0",
            scope="item",
            capability="local-coordination",
        ),
        ".linear",
    ),
    "linear": _BuiltinSpec(
        ResourcePolicyDescriptor(
            name="linear",
            origin="worklease",
            origin_version="0.1.0",
            scope="item",
            capability="local-coordination",
        ),
        ".linear",
    ),
    "markdown": _BuiltinSpec(
        ResourcePolicyDescriptor(
            name="markdown",
            origin="worklease",
            origin_version="0.1.0",
            scope="source",
            capability="source-claim",
        ),
        ".markdown",
    ),
}


def _policy_error(reason: str, **details: object) -> LeaseError:
    return LeaseError(
        reason,
        code=2,
        schemaVersion=RESOURCE_POLICY_CONTRACT_VERSION,
        **details,
    )


def _validate_descriptor(
    descriptor: object, *, expected_name: str, source: str
) -> ResourcePolicyDescriptor:
    if not isinstance(descriptor, ResourcePolicyDescriptor):
        raise _policy_error(
            "resource-policy-invalid-descriptor",
            provider=expected_name,
            source=source,
        )
    try:
        name = normalize_provider(descriptor.name)
    except LeaseError as error:
        raise _policy_error(
            "resource-policy-invalid-descriptor",
            provider=expected_name,
            source=source,
            field="name",
        ) from error
    if name != expected_name:
        raise _policy_error(
            "resource-policy-invalid-descriptor",
            provider=expected_name,
            source=source,
            field="name",
        )
    if not isinstance(descriptor.contract_version, int) or isinstance(
        descriptor.contract_version, bool
    ):
        raise _policy_error(
            "resource-policy-invalid-descriptor",
            provider=expected_name,
            source=source,
            field="contract_version",
        )
    if not isinstance(descriptor.key_policy_version, int) or isinstance(
        descriptor.key_policy_version, bool
    ):
        raise _policy_error(
            "resource-policy-invalid-descriptor",
            provider=expected_name,
            source=source,
            field="key_policy_version",
        )
    if descriptor.contract_version != RESOURCE_POLICY_CONTRACT_VERSION:
        raise _policy_error(
            "resource-policy-contract-version",
            provider=expected_name,
            source=source,
            contractVersion=descriptor.contract_version,
            supportedContractVersion=RESOURCE_POLICY_CONTRACT_VERSION,
        )
    for field in (
        "origin",
        "origin_version",
        "scope",
        "capability",
        "generic_execution_guarantee",
    ):
        value = getattr(descriptor, field)
        if not isinstance(value, str) or not value.strip():
            raise _policy_error(
                "resource-policy-invalid-descriptor",
                provider=expected_name,
                source=source,
                field=field,
            )
    if descriptor.scope not in {"item", "source"}:
        raise _policy_error(
            "resource-policy-invalid-descriptor",
            provider=expected_name,
            source=source,
            field="scope",
        )
    if descriptor.capability not in {
        "item-claim",
        "source-claim",
        "local-coordination",
    }:
        raise _policy_error(
            "resource-policy-invalid-descriptor",
            provider=expected_name,
            source=source,
            field="capability",
        )
    if descriptor.generic_execution_guarantee != "local-coordination":
        raise _policy_error(
            "resource-policy-invalid-descriptor",
            provider=expected_name,
            source=source,
            field="generic_execution_guarantee",
        )
    if not isinstance(descriptor.provider_fencing_supported, bool):
        raise _policy_error(
            "resource-policy-invalid-descriptor",
            provider=expected_name,
            source=source,
            field="provider_fencing_supported",
        )
    return descriptor


def _validate_registration(
    registration: object, *, expected_name: str, source: str
) -> ResourcePolicyRegistration:
    if isinstance(registration, tuple) and len(registration) == 2:
        registration = ResourcePolicyRegistration(
            descriptor=registration[0],  # type: ignore[arg-type]
            factory=registration[1],  # type: ignore[arg-type]
        )
    if not isinstance(registration, ResourcePolicyRegistration):
        raise _policy_error(
            "resource-policy-invalid-descriptor",
            provider=expected_name,
            source=source,
        )
    descriptor = _validate_descriptor(
        registration.descriptor, expected_name=expected_name, source=source
    )
    if not callable(registration.factory):
        raise _policy_error(
            "resource-policy-invalid-factory",
            provider=expected_name,
            source=source,
        )
    return ResourcePolicyRegistration(descriptor, registration.factory)


def _entry_points() -> tuple[Any, ...]:
    try:
        entries = metadata.entry_points()
        selected = entries.select(group=RESOURCE_POLICY_ENTRY_POINT_GROUP)
    except Exception as error:
        raise _policy_error(
            "resource-policy-discovery-failed",
            group=RESOURCE_POLICY_ENTRY_POINT_GROUP,
        ) from error
    return tuple(selected)


def _matching_entry_points(name: str) -> tuple[Any, ...]:
    matches: list[Any] = []
    for entry_point in _entry_points():
        entry_name = getattr(entry_point, "name", None)
        if not isinstance(entry_name, str):
            continue
        try:
            normalized = normalize_provider(entry_name)
        except LeaseError:
            continue
        if normalized == name:
            matches.append(entry_point)
    return tuple(matches)


def available_policy_names() -> tuple[str, ...]:
    """Return built-in and advertised external names without importing plugins."""

    names = set(_BUILTINS)
    for entry_point in _entry_points():
        value = getattr(entry_point, "name", None)
        if isinstance(value, str):
            try:
                names.add(normalize_provider(value))
            except LeaseError:
                continue
    return tuple(sorted(names))


def _builtin_registration(name: str) -> ResourcePolicyRegistration:
    spec = _BUILTINS[name]

    def factory(provider: str) -> ProviderAdapter:
        module = import_module(spec.module, __package__)
        candidate = getattr(module, "create_adapter", None)
        if not callable(candidate):
            raise _policy_error(
                "resource-policy-factory-unavailable",
                provider=name,
                source=f"builtin:{name}",
            )
        try:
            adapter = candidate(provider)
        except Exception as error:
            raise _policy_error(
                "resource-policy-factory-failed",
                provider=name,
                source=f"builtin:{name}",
            ) from error
        if not isinstance(adapter, ProviderAdapter):
            raise _policy_error(
                "resource-policy-invalid-factory",
                provider=name,
                source=f"builtin:{name}",
            )
        return adapter

    return ResourcePolicyRegistration(spec.descriptor, factory)


def _external_registration(
    name: str, matches: tuple[Any, ...] | None = None
) -> ResourcePolicyRegistration:
    matches = _matching_entry_points(name) if matches is None else matches
    if not matches:
        raise _policy_error(
            "resource-policy-not-found",
            provider=name,
            available=available_policy_names(),
        )
    if len(matches) != 1:
        raise _policy_error(
            "resource-policy-duplicate",
            provider=name,
            count=len(matches),
        )
    entry_point = matches[0]
    source = str(getattr(entry_point, "value", "entry-point"))
    try:
        loaded = cast(Any, entry_point).load()
    except Exception as error:
        raise _policy_error(
            "resource-policy-load-failed", provider=name, source=source
        ) from error
    return _validate_registration(loaded, expected_name=name, source=source)


def load_policy(provider: str) -> ResourcePolicyRegistration:
    """Resolve one policy by exact normalized name and validate its contract."""

    name = normalize_provider(provider)
    matches = _matching_entry_points(name)
    if name in _BUILTINS:
        if matches:
            raise _policy_error(
                "resource-policy-duplicate",
                provider=name,
                count=len(matches) + 1,
            )
        return _builtin_registration(name)
    return _external_registration(name, matches)


def load_adapter(provider: str) -> ProviderAdapter:
    """Load the selected policy's adapter, never falling back to another name."""

    name = normalize_provider(provider)
    registration = load_policy(name)
    try:
        adapter = registration.factory(name)
    except LeaseError:
        raise
    except Exception as error:
        raise _policy_error("resource-policy-factory-failed", provider=name) from error
    if not isinstance(adapter, ProviderAdapter):
        raise _policy_error("resource-policy-invalid-factory", provider=name)
    return adapter


__all__ = [
    "RESOURCE_POLICY_CONTRACT_VERSION",
    "RESOURCE_POLICY_ENTRY_POINT_GROUP",
    "ResourcePolicyDescriptor",
    "ResourcePolicyRegistration",
    "available_policy_names",
    "load_adapter",
    "load_policy",
]
