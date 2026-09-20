cat $env:LOCALAPPDATA\hit\history.jsonl | ` 
sql-cli -q "WITH r AS (SELECT * FROM read_jsonl('-')), `
     c AS (SELECT id, ts, cwd, cmd FROM r WHERE k = 'cmd'), `
     e AS (SELECT id, exit, ms FROM r WHERE k = 'end') `
SELECT c.ts, c.cwd, c.cmd, e.exit, e.ms `
FROM c LEFT JOIN e ON c.id = e.id `
ORDER BY c.ts"