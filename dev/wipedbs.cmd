@echo off

REM get repository root folder
FOR /F "tokens=*" %%g IN ('git rev-parse --show-toplevel') do (SET TOPLEVEL=%%g)

del /S /Q "%TOPLEVEL%\stakedb_ticket_pool.db*" "%TOPLEVEL%\dcrdata.sqlt.db"
rmdir /S /Q "%TOPLEVEL%\ffldb_stake"