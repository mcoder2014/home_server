function canPublishRelease(project, release) {
    if (!project || !release || release.status !== 'ready' || project.status === 'deleted' || (project.moderation_status && project.moderation_status !== 'normal')) {
        return false
    }
    return project.status !== 'enabled' || project.current_release_id !== release.id
}

function memberIDs(value) {
    return Array.from(new Set(value || [])).map(String).sort()
}

function hasUnsavedAccessChanges(project, form) {
    if (!project || !form || project.access_mode !== form.access_mode) {
        return true
    }
    if (project.access_mode !== 'members') {
        return false
    }
    return JSON.stringify(memberIDs(project.member_user_ids)) !== JSON.stringify(memberIDs(form.member_user_ids))
}

module.exports = {
    canPublishRelease,
    hasUnsavedAccessChanges,
}
