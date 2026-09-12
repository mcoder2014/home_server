# library

## Project setup
```
npm install
```

### Compiles and hot-reloads for development
```
npm run serve
```

### Compiles and minifies for production
```
npm run build
```

### Lints and fixes files
```
npm run lint
```

### Runs frontend behavior tests
```bash
npm run test:frontend
```

## Application credentials

Signed-in users manage their own application credentials at `/applications`. The page calls the same-origin `/api/applications` management API with the existing user `passport`; application Bearer identities are not accepted as a browser UI login.

| UI behavior | Security boundary |
| --- | --- |
| Create or rotate a credential | The returned Secret Key is held only in the open dialog state. |
| Copy or download credentials | A user must click the corresponding button; JSON contains only `access_key` and `secret_key`. |
| Close the credential dialog | The component clears the one-time credential state; the value is not written to local storage, URL, or logs. |
| Edit, enable, disable, rotate, or revoke | The current application revision is sent through `If-Match`. |

Write scopes automatically include the matching read scope. New applications default to `web-projects:read` and a 90-day lifetime.

### Customize configuration
See [Configuration Reference](https://cli.vuejs.org/config/).
