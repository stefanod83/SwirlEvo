export const perms = [
    {
        key: 'registry',
        actions: ['view', 'edit', 'delete'],
    },
    {
        key: 'node',
        actions: ['view', 'edit', 'delete'],
    },
    {
        key: 'network',
        actions: ['view', 'edit', 'delete', 'disconnect'],
    },
    {
        key: 'service',
        actions: ['view', 'edit', 'delete', 'deploy', 'restart', 'rollback', 'logs'],
    },
    {
        key: 'task',
        actions: ['view', 'logs'],
    },
    {
        key: 'stack',
        actions: ['view', 'edit', 'delete', 'deploy', 'shutdown'],
    },
    {
        key: 'config',
        actions: ['view', 'edit', 'delete'],
    },
    {
        key: 'secret',
        actions: ['view', 'edit', 'delete'],
    },
    {
        key: 'image',
        actions: ['view', 'edit', 'delete', 'push'],
    },
    {
        key: 'container',
        actions: ['view', 'edit', 'delete', 'logs', 'execute'],
    },
    {
        key: 'volume',
        actions: ['view', 'edit', 'delete'],
    },
    {
        key: 'user',
        actions: ['view', 'edit', 'delete'],
    },
    {
        key: 'role',
        actions: ['view', 'edit', 'delete'],
    },
    {
        key: 'chart',
        actions: ['view', 'edit', 'delete'],
    },
    {
        key: 'dashboard',
        actions: ['edit'],
    },
    {
        key: 'event',
        actions: ['view'],
    },
    {
        key: 'setting',
        actions: ['view', 'edit'],
    },
    {
        key: 'host',
        actions: ['view', 'edit', 'delete'],
    },
    {
        key: 'backup',
        actions: ['view', 'edit', 'delete', 'restore', 'download', 'recover'],
    },
    {
        key: 'vault',
        actions: ['admin'],
    },
    {
        key: 'vault_secret',
        actions: ['view', 'edit', 'delete', 'cleanup'],
    },
    {
        key: 'self_deploy',
        actions: ['view', 'edit', 'execute'],
    },
]
