# Proxy Group Routing Design

## Goal

Allow an account to select a proxy group so each new upstream request randomly uses one active, non-expired proxy from that group. Existing fixed-proxy and direct-connection accounts remain compatible.

## Architecture

Add a `proxy_groups` table and a many-to-many `proxy_group_proxies` join table. Add an optional `proxy_group_id` to accounts; this mode is mutually exclusive with the persisted fixed `proxy_id` at request time, while the existing fixed-proxy value remains available for compatibility and UI switching. The repository exposes active group members and the gateway resolves one member once before its retry loop, so all retries for a request use the same proxy and the next request can choose a different member.

The random selector is a small pure helper using an injected `math/rand` source in tests. Members are filtered by active status and expiration before selection. An empty or unavailable group returns a typed error and does not silently fall back to a fixed or direct connection.

## UI

The account edit form adds a proxy mode selector: direct, fixed proxy, or proxy group. Selecting a group sends `proxy_group_id` and clears `proxy_id`; selecting fixed proxy sends `proxy_id` and clears `proxy_group_id`; direct clears both. The account table displays the selected group name when group mode is active.

## Compatibility and Data Migration

The new account field is nullable and existing rows remain fixed-proxy or direct. Proxy groups are independent of existing account business groups. Deleting a group clears account selections but does not delete proxies. Deleting a proxy removes it from group membership; a group with no usable members fails explicitly at request time.

## Testing

Unit tests cover random selection only from eligible members, empty groups, and account update payload semantics. Gateway tests verify group resolution occurs once before retries and fixed-proxy behavior is unchanged. Existing backend and frontend test suites must remain green.
