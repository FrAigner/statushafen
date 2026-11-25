# GitLab as OIDC provider

> English version of the German guide [`../gitlab.md`](../gitlab.md).

Read [Global OIDC providers](providers.md) first. This page contains only the GitLab-specific steps,
for gitlab.com and for a self-hosted GitLab instance. GitLab sends `email_verified` in the ID token;
existing accounts with the same e-mail, confirmed in GitLab, are linked on the first login.

## gitlab.com

1. In GitLab, go to "Edit profile → Applications" (user application) or, in a group, to
   "Settings → Applications" (group application) and choose **Add new application**.
2. Name e.g. `Statushafen`, **Redirect URI**:

   ```text
   https://status.example.org/api/auth/provider/gitlab/callback
   ```

3. Tick **Confidential**, select the scopes **openid**, **email** and **profile**, save.
4. Copy the **Application ID** and the **Secret**.
5. Set in the backend environment:

   ```bash
   OIDC_PROVIDERS=gitlab
   OIDC_GITLAB_ISSUER=https://gitlab.com
   OIDC_GITLAB_CLIENT_ID=APPLICATION-ID
   OIDC_GITLAB_CLIENT_SECRET=SECRET
   OIDC_GITLAB_NAME=GitLab
   ```

6. Restart the backend. The login page shows "GitLab"; without a warning in the start-up log the
   provider is active.

With gitlab.com any GitLab account can sign in. Therefore restrict new accounts to your e-mail
domains with `SSO_ALLOWED_DOMAINS`.

## Self-hosted GitLab

The steps are the same, with the address of your instance:

- Create the application under "Admin Area → Applications" (instance-wide) or as above; **Trusted**
  spares users the consent dialog.
- The issuer is the base address of the instance, e.g. `https://gitlab.example.org` (for an
  installation under a path `https://example.org/gitlab`). Check:
  `https://gitlab.example.org/.well-known/openid-configuration` must be reachable, and its `issuer`
  field must equal `OIDC_GITLAB_ISSUER` exactly.
- The instance may live in the internal network; the backend must reach it via `https`, with a
  certificate the backend trusts (in the container: add the CA to the system store).

```bash
OIDC_PROVIDERS=gitlab
OIDC_GITLAB_ISSUER=https://gitlab.example.org
OIDC_GITLAB_CLIENT_ID=APPLICATION-ID
OIDC_GITLAB_CLIENT_SECRET=SECRET
OIDC_GITLAB_NAME=GitLab
```

Several instances work with their own IDs, e.g. `OIDC_PROVIDERS=gitlab,gitlab-internal`.

## As page SSO

GitLab can also be entered as the [page SSO](oidc.md) of a single status page (discovery URL =
issuer, redirect URI `…/api/auth/oidc/callback?page=<PAGE-ID>`).

Back to the [overview](README.md).
