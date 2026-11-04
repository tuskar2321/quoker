-- Indexes for retention and lookups. See docs/05-data-model.md.

-- Finished tasks are purged by age; admin statistics read recent ones.
CREATE INDEX tasks_updated ON tasks (updated_at);

-- Results find their send by task id (MarkSent, MarkFailed).
CREATE INDEX sends_task ON sends (task_id);
