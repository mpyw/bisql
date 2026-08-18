-- Schema and seed for the in-memory SQLite database the API serves.

create table departments (
    id   integer primary key,
    name text not null
);

create table users (
    id            integer primary key,
    name          text    not null,
    email         text    not null,
    age           integer not null,
    status        text    not null, -- 'active' | 'pending' | 'banned'
    department_id integer references departments (id)
);

create table user_tags (
    user_id integer not null references users (id),
    tag     text    not null
);

create table audit_logs (
    id         integer primary key autoincrement,
    user_id    integer not null references users (id),
    action     text    not null,
    created_at text    not null default (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

insert into departments (id, name) values (1, 'Engineering'), (2, 'Sales');

insert into users (id, name, email, age, status, department_id) values
    (1, 'Alice', 'alice@example.com', 30, 'active',  1),
    (2, 'Bob',   'bob@corp.example',  45, 'active',  1),
    (3, 'Carol', 'carol@example.com', 17, 'pending', 2),
    (4, 'Dave',  'dave@corp.example',  70, 'active',  2),
    (5, 'Erin',  'erin@example.com',  25, 'banned',  1);

insert into user_tags (user_id, tag) values
    (1, 'vip'), (1, 'beta'), (2, 'beta'), (4, 'vip');

insert into audit_logs (user_id, action, created_at) values
    (1, 'login',  '2025-03-01T09:00:00Z'),
    (1, 'logout', '2025-03-01T17:00:00Z'),
    (2, 'login',  '2025-06-15T08:30:00Z'),
    (3, 'login',  '2025-09-20T12:00:00Z'),
    (4, 'login',  '2025-12-31T23:59:00Z');
