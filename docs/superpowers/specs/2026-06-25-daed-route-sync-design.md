# Daed Route Sync Design

Date: 2026-06-25

## Goal

Add a button next to "检查并保存" that syncs all `route.rules[*].ip_cidr` entries from the current sing-box config into Daed routing rules.

The sync targets the Daed routing text block marked by `# 家 & kt` and the following `dip(...) -> singbox` rule.

## Configuration

Use environment variables only:

- `DAED_GRAPHQL_URL`
- `DAED_AUTHORIZATION`

`agh_session` cookie is not required and will not be supported in this version.

If either environment variable is missing, the API returns a clear error and the page displays which variable is missing.

## User Flow

1. User clicks "同步到 Daed".
2. Browser calls `POST /api/daed/sync-route-rules`.
3. Backend reads the current sing-box config through the existing config manager path.
4. Backend extracts all `ip_cidr` values from `route.rules`.
5. Backend queries Daed `routings`.
6. Backend selects the routing where `selected=true`.
7. Backend locates `# 家 & kt` and the following `dip(...) -> singbox` block.
8. Backend compares existing Daed `dip` IPs with sing-box IPs.
9. If no IPs are missing, the API returns a no-change result.
10. If IPs are missing, backend replaces the block with the merged IP list, calls `updateRouting`, then calls `run(dry:false)`.
11. Page displays success, no-change, or error details.

## Data Rules

- `ip_cidr` may be a string or an array of strings.
- Empty values are ignored.
- Duplicate values are removed.
- Ordering keeps existing Daed IP order first, then appends missing sing-box IPs in route rule order.
- Non-IP route rules and rules without `ip_cidr` are ignored.
- The sync only updates the first `dip(...) -> singbox` block after `# 家 & kt`.
- If `# 家 & kt` or the target `dip(...) -> singbox` block cannot be found, the sync fails without modifying Daed.

## API

Endpoint:

- `POST /api/daed/sync-route-rules`

Success response:

```json
{
  "routingId": "Y3Vyc29yMQ",
  "routingName": "default",
  "changed": true,
  "existingCount": 10,
  "sourceCount": 12,
  "added": ["10.0.0.0/24", "192.168.1.1/32"],
  "message": "已同步 2 个 IP 到 Daed"
}
```

No-change response:

```json
{
  "routingId": "Y3Vyc29yMQ",
  "routingName": "default",
  "changed": false,
  "existingCount": 12,
  "sourceCount": 12,
  "added": [],
  "message": "Daed 已是最新，无需更新"
}
```

Error responses include a human-readable `error` field.

## Daed GraphQL

Query selected routing:

```graphql
query Routings {
  routings {
    id
    name
    selected
    routing {
      string
    }
  }
}
```

Update routing:

```graphql
mutation UpdateRouting($id: ID!, $routing: String!) {
  updateRouting(id: $id, routing: $routing) {
    id
  }
}
```

Reload Daed:

```graphql
mutation Run($dry: Boolean!) {
  run(dry: $dry)
}
```

## Error Handling

- Missing `DAED_GRAPHQL_URL`: return HTTP 400.
- Missing `DAED_AUTHORIZATION`: return HTTP 400.
- Sing-box config cannot be loaded: return HTTP 500.
- No selected Daed routing: return HTTP 422.
- Missing `# 家 & kt` marker: return HTTP 422.
- Missing following `dip(...) -> singbox` block: return HTTP 422.
- Daed GraphQL request failure: return HTTP 502 with the GraphQL or network error.

## Testing

Backend unit tests cover:

- Extracting `ip_cidr` from string and array route rules.
- Deduplicating while preserving order.
- Replacing only the `dip(...) -> singbox` block after `# 家 & kt`.
- No-change detection.
- Missing marker and missing block errors.
- Missing environment configuration errors.
- GraphQL request sequence: query, update, run.

Manual verification covers:

- Button appears next to "检查并保存".
- Missing environment variables show a page-level error.
- A no-change response shows "Daed 已是最新，无需更新".
- A changed response shows added count and routing name.
