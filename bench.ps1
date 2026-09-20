# bench.ps1
# Защита кодировки: принудительно переводим консоль PowerShell в режим UTF-8
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$OutputEncoding = [System.Text.Encoding]::UTF8

# 1. Проверяем и при необходимости устанавливаем benchstat
if (-not (Get-Command benchstat -ErrorAction SilentlyContinue)) {
    Write-Host "[*] Утилита benchstat не найдена. Устанавливаю..." -ForegroundColor Cyan
    go install golang.org/x/perf/cmd/benchstat@latest
    $env:PATH += ";$env:USERPROFILE\go\bin"
}

# 2. Создаем структуру папок для хранения логов бенчмарков
$BenchDir = Join-Path $PSScriptRoot "benchmarks"
if (-not (Test-Path $BenchDir)) {
    New-Item -ItemType Directory -Path $BenchDir | Out-Null
    Write-Host "[+] Создана директория для статистики: $BenchDir" -ForegroundColor Green
}

# 3. Запрашиваем у пользователя алиас текущего состояния кода
Write-Host "=== Запуск тестирования хелперов ===" -ForegroundColor Magenta
$Alias = Read-Host "Введите короткий алиас для этого прогона (например: 'baseline', 'pointer_fix')"
if ([string]::IsNullOrWhiteSpace($Alias)) {
    $Alias = "unnamed"
}

# Очищаем алиас от недопустимых в именах файлов символов
$SafeAlias = $Alias -replace '[\\/:*?"<>| ]', '_'
$CurrentDate = Get-Date -Format "yyyyMMdd_HHmmss"
$FileName = "${CurrentDate}_${SafeAlias}.txt"
$CurrentFile = Join-Path $BenchDir $FileName

# 4. Запуск бенчмарков
Write-Host "`n[*] Запускаю бенчмарки пакета handler (10 итераций для надежной статистики)..." -ForegroundColor Yellow
# Запускаем тесты, содержащие Stage
go test -bench=Stage -benchmem -count=10 ./internal/handler | Out-File -FilePath $CurrentFile -Encoding utf8

Write-Host "[+] Результаты успешно сохранены в: .\benchmarks\$FileName" -ForegroundColor Green

# 5. Автоматическое сравнение с предыдущими результатами
$PastFiles = Get-ChildItem -Path $BenchDir -Filter "*.txt" | Where-Object { $_.FullName -ne $CurrentFile }

if ($PastFiles.Count -eq 0) {
    Write-Host "`n[!] Это первый сохраненный прогон в папке benchmarks. Сравнивать пока не с чем." -ForegroundColor Yellow
} else {
    Write-Host "`n=== Сравнение текущего прогона [\$SafeAlias] со старыми результатами ===" -ForegroundColor Green
    
    foreach ($OldFile in $PastFiles) {
        Write-Host "`n--------------------------------------------------" -ForegroundColor Gray
        Write-Host "Сравнение с базой: $($OldFile.Name)" -ForegroundColor Cyan
        Write-Host "--------------------------------------------------" -ForegroundColor Gray
        
        # Запускаем benchstat для пары: старый файл vs новый файл
        benchstat $OldFile.FullName $CurrentFile
    }
}

Write-Host "`n[+] Работа скрипта успешно завершена." -ForegroundColor Magenta
