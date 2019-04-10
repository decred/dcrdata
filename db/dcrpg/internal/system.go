package internal

const (
	RetrieveSysSettingsConfFile = `SELECT name, setting, unit, short_desc, source, sourcefile, sourceline
		FROM pg_settings
		WHERE source='configuration file';`

	RetrieveSysSettingsServer = `SELECT name, setting, unit, short_desc, source, sourcefile, sourceline
		FROM pg_settings
		WHERE name='max_connections'
			OR name='timezone'
			OR name='max_files_per_process'
			OR name='dynamic_shared_memory_type'
			OR name='unix_socket_directories'
			OR name='port'
			OR name='data_directory'
			OR name='config_file'
			OR name='listen_address';`

	RetrieveSysSettingsPerformance = `SELECT name, setting, unit, short_desc, source, sourcefile, sourceline
		FROM pg_settings
		WHERE name='synchronous_commit'
			OR name='max_connections'
			OR name='shared_buffers'
			OR name='effective_cache_size'
			OR name='maintenance_work_mem'
			OR name='work_mem'
			OR name='autovacuum_work_mem'
			OR name='wal_buffers'
			OR name='min_wal_size'
			OR name='max_wal_size'
			OR name='wal_level'
			OR name='checkpoint_completion_target'
			OR name='default_statistics_target'
			OR name='random_page_cost'
			OR name='seq_page_cost'
			OR name='effective_io_concurrency'
			OR name='max_worker_processes'
			OR name='max_parallel_workers_per_gather'
			OR name='max_parallel_workers'
			OR name='autovacuum'
			OR name='fsync'
			OR name='full_page_writes'
			OR name='huge_pages'
			OR name='temp_buffers'
			OR name='max_stack_depth'
			OR name='force_parallel_mode'
			OR name='jit'
			OR name='jit_provider';`

	RetrieveSyncCommitSetting = `SELECT setting FROM pg_settings WHERE name='synchronous_commit';`

	RetrievePGVersion = `SELECT version();`
)
