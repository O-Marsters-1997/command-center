(
	idle=0
	while sleep 10; do
		n=$(psql -U "$POSTGRES_USER" -d postgres -tAc \
			"SELECT count(*) FROM pg_stat_activity WHERE backend_type = 'client backend' \
			AND pid <> pg_backend_pid()" 2>/dev/null) || { idle=0; continue; }
		if [ "$n" = 0 ]; then idle=$((idle + 10)); else idle=0; fi
		if [ "$idle" -ge "$CCTEST_IDLE_SECONDS" ]; then kill -INT 1; fi
	done
) &
exec docker-entrypoint.sh "$@"
