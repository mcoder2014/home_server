const assert = require('node:assert/strict')
const test = require('node:test')

const {
    clearOneTimeCredential,
    createOneTimeCredential,
    credentialJSON,
    handleIdentityFailure,
    normalizeScopes,
} = require('../src/utils/applications_behavior.cjs')

test('write scopes always include the matching read scope in stable order', () => {
    assert.deepEqual(normalizeScopes(['library:write', 'web-comments:write', 'web-projects:write', 'library:write']), [
        'web-comments:read',
        'web-comments:write',
        'web-projects:read',
        'web-projects:write',
        'library:read',
        'library:write',
    ])
})

test('new and rotated secrets exist only in an explicit one-time credential state', () => {
    const credential = createOneTimeCredential({
        id: '12',
        name: 'deploy bot',
        access_key: 'ak_cq_example',
    }, 'sk_cq_once')

    assert.deepEqual(credential, {
        applicationID: '12',
        applicationName: 'deploy bot',
        accessKey: 'ak_cq_example',
        secretKey: 'sk_cq_once',
    })
    assert.deepEqual(JSON.parse(credentialJSON(credential)), {
        access_key: 'ak_cq_example',
        secret_key: 'sk_cq_once',
    })
})

test('closing the credential dialog overwrites both credential values before dropping state', () => {
    const credential = createOneTimeCredential({
        id: '12',
        name: 'deploy bot',
        access_key: 'ak_cq_example',
    }, 'sk_cq_once')

    const nextState = clearOneTimeCredential(credential)

    assert.equal(nextState, null)
    assert.equal(credential.accessKey, '')
    assert.equal(credential.secretKey, '')
})

test('missing secret never creates a downloadable credential state', () => {
    assert.equal(createOneTimeCredential({id: '12', access_key: 'ak_cq_example'}, ''), null)
    assert.equal(credentialJSON(null), '')
})

test('an unauthorized response clears the user identity before navigating to login', () => {
    const events = []

    const handled = handleIdentityFailure(
        {status: 401},
        () => events.push('clear'),
        () => events.push('login'),
    )

    assert.equal(handled, true)
    assert.deepEqual(events, ['clear', 'login'])
})

test('a forbidden application operation keeps the current user identity', () => {
    const events = []

    const handled = handleIdentityFailure(
        {status: 403},
        () => events.push('clear'),
        () => events.push('login'),
    )

    assert.equal(handled, false)
    assert.deepEqual(events, [])
})
