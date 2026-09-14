const {createBrowserRequest} = require('./browser_client.cjs')

// 统一封装登录、个人资料、邀请和管理接口；共用浏览器 Cookie/CSRF 请求器，并传递写入所需的修订号。
function createAccountsApi(transport, getCSRF) {
    const request = createBrowserRequest(transport, getCSRF)
    const id = encodeURIComponent
    return {
        bootstrap: () => request('get', '/api/site/bootstrap'),
        me: () => request('get', '/api/auth/me'),
        login: data => request('post', '/api/auth/login', data),
        logout: () => request('post', '/api/auth/logout'),
        logoutAll: () => request('post', '/api/auth/logout-all'),
        changePassword: data => request('post', '/api/auth/change-password', data),
        profile: (revision, data) => request('patch', '/api/account/profile', data, revision),
        registrationPolicy: () => request('get', '/api/auth/registration-policy'),
        validateInvitation: code => request('post', '/api/auth/invitations/validate', {code}),
        register: data => request('post', '/api/auth/register', data),
        invitations: () => request('get', '/api/account/invitations'),
        createInvitation: data => request('post', '/api/account/invitations', data),
        revokeInvitation: invitationID => request('post', `/api/account/invitations/${id(invitationID)}/revoke`),
        users: params => request('get', '/api/admin/users', undefined, undefined, params),
        user: userID => request('get', `/api/admin/users/${id(userID)}`),
        createUser: data => request('post', '/api/admin/users', data),
        userAction: (userID, action, revision, data) => request(['role', 'library-permission', 'webdav-permission'].includes(action) ? 'put' : 'post', `/api/admin/users/${id(userID)}/${action}`, data, revision),
        revokeUserInvitation: (userID, invitationID, revision, data) => request('post', `/api/admin/users/${id(userID)}/invitations/${id(invitationID)}/revoke`, data, revision),
        projects: params => request('get', '/api/admin/web-share', undefined, undefined, params),
        project: projectID => request('get', `/api/admin/web-share/${id(projectID)}`),
        projectAction: (projectID, action, revision, data) => request('post', `/api/admin/web-share/${id(projectID)}/${action}`, data, revision),
        auditLogs: params => request('get', '/api/admin/audit-logs', undefined, undefined, params),
        configSchema: () => request('get', '/api/admin/config/schema'),
        configs: () => request('get', '/api/admin/config'),
        config: namespace => request('get', `/api/admin/config/${id(namespace)}`),
        configStatus: () => request('get', '/api/admin/config/status'),
        validateConfig: (namespace, values) => request('post', `/api/admin/config/${id(namespace)}/validate`, {values}),
        publishConfig: (namespace, revision, data) => request('put', `/api/admin/config/${id(namespace)}`, data, revision),
        configHistory: (namespace, params) => request('get', `/api/admin/config/${id(namespace)}/history`, undefined, undefined, params),
        rollbackConfig: (namespace, revision, data) => request('post', `/api/admin/config/${id(namespace)}/rollback`, data, revision),
    }
}

const accountsApi = createAccountsApi()
module.exports = {createAccountsApi, accountsApi}
