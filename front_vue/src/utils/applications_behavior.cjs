const SCOPES = [
    'web-projects:read',
    'web-projects:write',
    'library:read',
    'library:write',
    'webdav:read',
    'webdav:write',
]

function normalizeScopes(scopes) {
    const selected = new Set(scopes || [])
    for (const scope of Array.from(selected)) {
        if (scope.endsWith(':write')) {
            selected.add(scope.replace(':write', ':read'))
        }
    }
    return SCOPES.filter((scope) => selected.has(scope))
}

function createOneTimeCredential(application, secretKey) {
    if (!application || !application.access_key || !secretKey) {
        return null
    }
    return {
        applicationID: String(application.id || ''),
        applicationName: application.name || '',
        accessKey: application.access_key,
        secretKey,
    }
}

function clearOneTimeCredential(credential) {
    if (credential) {
        credential.accessKey = ''
        credential.secretKey = ''
    }
    return null
}

function credentialJSON(credential) {
    if (!credential || !credential.accessKey || !credential.secretKey) {
        return ''
    }
    return JSON.stringify({
        access_key: credential.accessKey,
        secret_key: credential.secretKey,
    }, null, 2)
}

function handleIdentityFailure(error, clearIdentity, navigateToLogin) {
    if (!error || error.status !== 401) {
        return false
    }
    clearIdentity()
    navigateToLogin()
    return true
}

module.exports = {
    SCOPES,
    clearOneTimeCredential,
    createOneTimeCredential,
    credentialJSON,
    handleIdentityFailure,
    normalizeScopes,
}
