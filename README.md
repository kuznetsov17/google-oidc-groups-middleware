# Traefik Google OIDC Auth Middleware

This is a Traefik middleware plugin that authenticates users with Google OpenID
Connect, and then checks that their email address, Google Workspace domain, or 
Google Group membership is authorized.

## Requirements

- Setup a new project in the Google API console to obtain a client ID and 
client secret. See the [Google developer docs](https://developers.google.com/identity/openid-connect/openid-connect).
- Install the plugin to Traefik using static config.
- Configure the middleware in dynamic config.
- Associate a service to the middleware.

## Configuration

| Option             | Default        | Required | Description                                                                                                                                                                       |
|--------------------|----------------|----------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| oidc.clientID      |                | X        | OAuth client ID                                                                                                                                                                   |
| oidc.clientSecret  |                | X        | OAuth client secret                                                                                                                                                               |
| oidc.callbackPath  | /oidc/callback |          | The path where the OIDC provider will redirect the user after authenticating.                                                                                                     |
| oidc.redirectHost  |                |          | Optional host override for the OIDC redirect URI. Use this to configure a single, central redirect URI for multiple subdomains (e.g., `auth.example.com`). Requires `cookie.domain` to be set for cookie sharing. |
| oidc.prompt        |                |          | A space-delimited, case-sensitive list of prompts to present the user. Possible values are: `none`, `consent`, `select_account`. See [Google's docs](https://developers.google.com/identity/protocols/oauth2/web-server#httprest_1) for more info. |
| cookie.name        | oidc_auth      |          | Name of the cookie. It can be customized to avoid collisions when running multiple instances of the middleware.                                                                   |
| cookie.path        | /              |          | You can use this to limit the scope of the cookie to a specific path. Defaults to '/'.                                                                                            |
| cookie.secret      |                | X        | Secret is the HMAC key for cookie signing, and helps provide integrity protection for cookies.                                                                                    |
| cookie.duration    | 24h            |          | Validity period for new cookies. Users are granted access for this length of time regardless of changes to user's account in the OIDC provider. Uses the Go time.Duration format. |
| cookie.insecure    | false          |          | Set to true to omit the `Secure` attribute from cookies.                                                                                                                          |
| cookie.sameSite    | Lax            |          | SameSite attribute for cookies. Options: `Strict`, `Lax`, `None`. `Lax` provides CSRF protection while allowing cookies on top-level navigation.                                  |
| cookie.domain      |                |          | Domain attribute for cookies. Use this to share cookies across subdomains (e.g., `.example.com`). Must start with a dot. Required when using `oidc.redirectHost`.                  |
| authorized.emails  |                |          | List of allowed email addresses.                                                                                                                                                  |
| authorized.domains |                |          | List of allowed domains.                                                                                                                                                          |
| authorized.groups  |                |          | List of allowed Google Group names. Requires `GOOGLE_SERVICE_ACCOUNT_JSON` and `GOOGLE_GROUPS_SUBJECT` environment variables to be set.                                             |
| debug              | false          |          | Enable debug logging to stdout.

**Note:** At least one of `authorized.emails`, `authorized.domains`, or `authorized.groups` must be configured.

## Environment Variables for Google Groups

When using `authorized.groups`, the following environment variables must be set:

| Variable | Description |
|----------|-------------|
| `GOOGLE_SERVICE_ACCOUNT_JSON` | Full JSON content of the Google Cloud service account key file with domain-wide delegation enabled. Required when `authorized.groups` is configured. |
| `GOOGLE_GROUPS_SUBJECT` | Email address of a Google Workspace admin user for domain-wide delegation impersonation. Required when `authorized.groups` is configured. |

### Setup Instructions

1. **Create a service account** in Google Cloud Console with the following scopes:
   - `https://www.googleapis.com/auth/admin.directory.group.readonly`
   - `https://www.googleapis.com/auth/admin.directory.user.readonly`

2. **Enable domain-wide delegation** on the service account in Google Cloud Console.

3. **Grant API access** in Google Workspace Admin:
   - Go to Security → API controls → Domain-wide delegation
   - Add the service account with the scopes listed above

4. **Pass credentials to Traefik:**
   ```bash
   export GOOGLE_SERVICE_ACCOUNT_JSON='<full JSON content of service account key>'
   export GOOGLE_GROUPS_SUBJECT='admin@yourdomain.com'
   ```

5. **Find group names** in Google Workspace Admin (Groups section) — use the display name (e.g., "Developers", "Admins"), not the email address.

## Headers

*X-Forwarded-User*

When the middleware proxies a request it adds an `X-Fowarded-User` header
containing the user's email address. This can be used by the downstream service
to identify the authenticated user.

If you want your JSON access logs to include the user's email address then
configure the access log to retain the `X-Forwarded-User` header. Here is a
CLI example:

```
# Adding X-Forwarded-User to JSON access logs.
--accesslog
--accesslog.format=json
--accesslog.fields.headers.names.X-Forwarded-User=keep
```

The resulting access log will contain a `request_X-Forwarded-User` field.

```json
    "request_X-Forwarded-User": "name@gmail.com"
```

See [Limiting the Fields/Including Headers](https://doc.traefik.io/traefik/observability/access-logs/#limiting-the-fieldsincluding-headers) for more details.


## Example config

Static config

```yaml
# traefik.yml

experimental:
  plugins:
    google-oidc-auth-middleware:
      moduleName: "github.com/andrewkroh/google-oidc-auth-middleware"
      # Populate this with the latest release tag.
      version: vX.Y.Z
```

Dynamic config

```yaml
# dynamic.yml

http:
  middlewares:
    oidc-auth:
      plugin:
        google-oidc-auth-middleware:
          oidc:
            clientID: example.apps.googleusercontent.com
            clientSecret: fake-secret
          cookie:
            secret: mySecretKey
          authorized:
            emails:
              - name@gmail.com
            domains:
              - example.com
            groups:
              - Developers
              - Admins
  routers:
    my-router:
      rule: host(`localhost`)
      service: service-foo
      entryPoints:
        - web
      middlewares:
        - oidc-auth
```

In this example, a user is allowed if they have **any** of:
- Email address `name@gmail.com`
- Domain `example.com`
- Group membership in `Developers` or `Admins` (requires `GOOGLE_SERVICE_ACCOUNT_JSON` and `GOOGLE_GROUPS_SUBJECT` env vars)

## Multi-Subdomain Configuration

When protecting multiple subdomains (e.g., `app1.example.com`, `app2.example.com`, `app3.example.com`) under the same parent domain, you can configure a single central redirect URI instead of registering each subdomain individually with your OAuth provider.

### Configuration

This feature requires two settings:

1. **`oidc.redirectHost`**: Set this to a central host that will handle all OIDC callbacks (e.g., `auth.example.com`)
2. **`cookie.domain`**: Set this to share cookies across all subdomains (e.g., `.example.com`)

### Example

```yaml
# dynamic.yml

http:
  middlewares:
    oidc-auth:
      plugin:
        google-oidc-auth-middleware:
          oidc:
            clientID: example.apps.googleusercontent.com
            clientSecret: fake-secret
            redirectHost: auth.example.com  # Central callback host
            callbackPath: /oidc/callback
          cookie:
            secret: mySecretKey
            domain: .example.com  # Share cookies across *.example.com
          authorized:
            emails:
              - name@gmail.com
            domains:
              - example.com

  routers:
    # Router for the central callback host
    auth-callback:
      rule: Host(`auth.example.com`) && Path(`/oidc/callback`)
      service: noop@internal
      entryPoints:
        - web
      middlewares:
        - oidc-auth
      #tls: ...

    # Routers for protected subdomains
    app1:
      rule: Host(`app1.example.com`)
      service: service-app1
      entryPoints:
        - web
      middlewares:
        - oidc-auth

    app2:
      rule: Host(`app2.example.com`)
      service: service-app2
      entryPoints:
        - web
      middlewares:
        - oidc-auth

    app3:
      rule: Host(`app3.example.com`)
      service: service-app3
      entryPoints:
        - web
      middlewares:
        - oidc-auth
```

### Google OAuth Setup

In your Google OAuth console, you only need to register **one** authorized redirect URI:

```
https://auth.example.com/oidc/callback
```

Instead of having to register:
- `https://app1.example.com/oidc/callback`
- `https://app2.example.com/oidc/callback`
- `https://app3.example.com/oidc/callback`

### How It Works

1. User visits `https://app1.example.com`
2. Middleware redirects to Google OAuth with `redirect_uri=https://auth.example.com/oidc/callback`
3. User authenticates with Google
4. Google redirects to `https://auth.example.com/oidc/callback`
5. Middleware sets a cookie with `Domain=.example.com` (shared across all subdomains)
6. Middleware redirects user back to original URL: `https://app1.example.com`
7. User can now access any subdomain without re-authenticating (cookie is shared)

### Requirements

- All protected sites must be under the same eTLD+1 (e.g., `*.example.com`)
- Sharing cookies across different apex domains (e.g., `example.com` vs `example.org`) is not supported

## Google Groups Authorization

The middleware can check if a user is a member of specific Google Groups and allow/deny access based on group membership. This is useful for more granular access control.

### How It Works

1. User authenticates via Google OIDC
2. Middleware fetches the user's Google Groups using a service account with domain-wide delegation
3. Extracts the group names (display names, not email addresses)
4. Checks if any of the user's groups match the configured allowed groups
5. Caches group names in the signed authentication cookie for the cookie duration (default 24h)
6. Subsequent requests are authorized from the cached cookie (no repeated API calls)

### Authorization Logic

- **OR semantics**: A user is authorized if they match **any** of:
  - Email address in `authorized.emails`
  - Domain in `authorized.domains`
  - Group name in `authorized.groups`

This means you can use groups, emails, and domains together flexibly.

### Configuration Example

```yaml
# dynamic.yml

http:
  middlewares:
    oidc-auth-with-groups:
      plugin:
        google-oidc-auth-middleware:
          oidc:
            clientID: example.apps.googleusercontent.com
            clientSecret: fake-secret
          cookie:
            secret: mySecretKey
          authorized:
            groups:
              - Engineering
              - Product
              - Leadership
```

With environment variables:
```bash
export GOOGLE_SERVICE_ACCOUNT_JSON='{"type":"service_account",...}'
export GOOGLE_GROUPS_SUBJECT='admin@example.com'
```

### Getting Group Names

Group names are the **display names** shown in Google Workspace Admin console (not email addresses). For example:
- ✅ Correct: `"Developers"`, `"Engineering Team"`, `"Contractors"`
- ❌ Incorrect: `"developers@example.com"`, `"engineers@example.com"`

To find your group names:
1. Go to Google Workspace Admin Console (`admin.google.com`)
2. Navigate to Users and access → Groups
3. Look at the "Name" column (not the "Email" column)

### Performance Notes

- Group membership is fetched **once during login** via the Google Admin API
- Results are cached in the signed cookie for the cookie duration (default 24h)
- No API calls are made for subsequent requests; authorization is checked from the cached cookie
- To refresh group membership, users must clear their cookies or wait for cookie expiry
