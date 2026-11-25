# Getting started

## Order a status page

"Order your own status page" on the home page (address `/order`) lets you create a status page
yourself. It takes three steps:

1. **Fill in the form:** "Company / page name" and "Email" are required. Under "Desired address
   (optional)" you can pick the slug, i.e. the part in front of `.example.com`. If you leave
   it empty, the slug is generated from the name. Lowercase letters, digits and single hyphens
   are allowed; Statushafen rejects reserved names such as `www` or `login`, the list is under
   [Status pages](/docs/statusseiten#address-of-a-status-page).
2. **Confirm your email:** Statushafen sends a confirmation link. Only when you click it are the
   status page and the user account with the role "Page Admin" created.
3. **Set a password:** You then set your password (at least 8 characters) and log in as usual.
   There is no automatic login.

Good to know:

- Both links are valid for 24 hours. Unconfirmed orders expire.
- At most three open orders are possible per email address.
- If an account already exists for the email address, the confirmation is rejected.
- A global admin can switch ordering off in the [System settings](/docs/systemeinstellungen).

Without self-service, a global admin creates status pages under **Status Pages** in the admin
area.

## Log in

Login lives under `/login` ("Login" button at the top right). The operator decides which ways the
login page offers:

- **Sign-in providers:** one "Sign in with <name>" button per active sign-in provider, e.g. for
  GitLab.
- **Email and password:** only if the operator has enabled this kind of sign-in.
- **The status page's SSO:** if you come from a status page that has OIDC set up, the login page
  also shows "Sign in with SSO". Details in [SSO](/docs/sso).

After signing in you go back to the page you came from, for example the status page. The
"Administration" link at the bottom of every public page leads into the admin area. If you open an
address in the admin area directly, you also end up there after signing in.

## After your first login

1. Pick the status page at the top of the header ("Tenant" field) if you have several.
2. Create the services the page should show under **Components**. Services of a cloud provider
   can be adopted under **External sources**, see [External sources](/docs/externe-quellen).
3. Check the public page via the user menu and "Public View".
4. When something breaks, report an incident under **Incidents**, see [Incidents](/docs/vorfaelle).

## The admin area

- **Header:** the tenant switcher on the left; light/dark mode, language selection and the user
  menu on the right. On narrow screens the menu icon on the left of the header opens the
  navigation.
- **Areas:** below it are the tabs **Operations**, **Content**, **Management** and **System**.
  A click shows the menu items of that area. Items without the matching permission are hidden,
  see [Users, roles and permissions](/docs/benutzer-und-rollen).
- **User menu:** shows your email and role on the selected page and contains the switch "Show
  help tooltips", "Profile", "Public View" and "Logout".
- **Help tooltips:** if you pause on a field or button, a hint explains what the field does, what
  follows from it, and gives an example. The hints are available on every page of the "Operations"
  area (Overview, Components, External sources, Incidents, Maintenance windows, Notices,
  Templates, Subscribers). Every user can switch this off in the user menu.
- **Outage banner:** if an [external source](/docs/externe-quellen#outage-of-a-source) fails, a
  notice with a link to the sources page appears at the top of the content area.
- **Documentation:** the "Documentation" item in the System area opens this page.

On the public status page, the entry "Dashboard" in the user menu leads to the admin area. It only
appears when your role on exactly this status page has the `admin_dashboard` permission; a role on
another page is not enough (global admins always see it).
