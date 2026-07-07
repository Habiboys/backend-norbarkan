@echo off
start "Air Backend" cmd /c "cd /d d:\Nouval\norbarkan\backend-nobarkan && taskkill /f /im main.exe 2>nul && air"
exit 0