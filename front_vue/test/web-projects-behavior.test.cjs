const assert = require('node:assert/strict')
const test = require('node:test')

const {
    canPublishRelease,
    hasUnsavedAccessChanges,
} = require('../src/utils/web_projects_behavior.cjs')

test('allows the current ready release to bring a disabled project online again', () => {
    const currentRelease = {id: '9', status: 'ready'}

    assert.equal(canPublishRelease({status: 'disabled', current_release_id: '9'}, currentRelease), true)
    assert.equal(canPublishRelease({status: 'enabled', current_release_id: '9'}, currentRelease), false)
    assert.equal(canPublishRelease({status: 'deleted', current_release_id: '9'}, currentRelease), false)
})

test('detects unsaved visibility and member changes before publishing', () => {
    const project = {access_mode: 'public', member_user_ids: []}

    assert.equal(hasUnsavedAccessChanges(project, {access_mode: 'owner', member_user_ids: []}), true)
    assert.equal(hasUnsavedAccessChanges(project, {access_mode: 'public', member_user_ids: []}), false)
    assert.equal(
        hasUnsavedAccessChanges(
            {access_mode: 'members', member_user_ids: ['2', '1']},
            {access_mode: 'members', member_user_ids: ['1', '2']},
        ),
        false,
    )
})
