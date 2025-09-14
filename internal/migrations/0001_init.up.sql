create table if not exists traces (
  id text primary key,
  created_at timestamptz default now(),
  developer jsonb not null,
  task jsonb not null,
  environment jsonb not null,
  status text not null default 'open',
  upload_token_hash text not null,
  artifacts jsonb,
  qa jsonb,
  version text not null default 'trace-1.0.0'
);

create table if not exists event_batches (
  id text primary key,
  trace_id text references traces(id),
  seq bigint not null,
  object_ref text not null,
  created_at timestamptz default now(),
  event_count bigint not null
);

create index if not exists idx_event_batches_trace_seq on event_batches(trace_id, seq);
