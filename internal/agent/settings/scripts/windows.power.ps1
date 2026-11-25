    # Plan de energía, tiempos de pantalla y suspensión, e hibernación.
    # Solo se comprueba y aplica lo que trae la política.
    $plans = @{ balanced = '381b4222-f694-41f0-9685-ff5bb260df2e'; high_performance = '8c5e7fda-e8bf-4a96-9a85-a6e23a8c635c'; power_saver = 'a1841308-3541-4fab-bc81-f71556f20b4a' }
    $planNames = @{ balanced = 'Equilibrado'; high_performance = 'Alto rendimiento'; power_saver = 'Economizador' }
    $plan = Get-Param 'PLAN'
    $hib = Get-Param 'HIBERNATE'
    $timeouts = @(
        @{ Param = 'MONITOR_AC'; Sub = 'SUB_VIDEO'; Name = 'VIDEOIDLE'; Change = 'monitor-timeout-ac'; Index = 0; Text = 'pantalla enchufado' },
        @{ Param = 'MONITOR_DC'; Sub = 'SUB_VIDEO'; Name = 'VIDEOIDLE'; Change = 'monitor-timeout-dc'; Index = 1; Text = 'pantalla con batería' },
        @{ Param = 'SLEEP_AC'; Sub = 'SUB_SLEEP'; Name = 'STANDBYIDLE'; Change = 'standby-timeout-ac'; Index = 0; Text = 'suspensión enchufado' },
        @{ Param = 'SLEEP_DC'; Sub = 'SUB_SLEEP'; Name = 'STANDBYIDLE'; Change = 'standby-timeout-dc'; Index = 1; Text = 'suspensión con batería' }
    )

    # Los dos últimos valores hexadecimales de «powercfg /query» son el índice
    # actual con corriente y con batería, sea cual sea el idioma de Windows.
    function Get-PowerMinutes([string]$Sub, [string]$Name, [int]$Index) {
        $out = & powercfg.exe /query SCHEME_CURRENT $Sub $Name
        $hex = @($out | Select-String -Pattern '0x[0-9a-fA-F]{8}' -AllMatches | ForEach-Object { $_.Matches } | ForEach-Object { $_.Value })
        if ($hex.Count -lt 2) { return $null }
        $values = $hex[($hex.Count - 2)..($hex.Count - 1)]
        return [int][math]::Floor([Convert]::ToInt64($values[$Index], 16) / 60)
    }

    if ($Mode -eq 'Set') {
        if ($plan) {
            & powercfg.exe /setactive $plans[$plan] | Out-Null
            if ($LASTEXITCODE -ne 0) { throw "El plan «$($planNames[$plan])» no existe en este equipo." }
        }
        foreach ($t in $timeouts) {
            $v = Get-Param $t.Param
            if ($null -eq $v) { continue }
            & powercfg.exe /change $t.Change ([int]$v) | Out-Null
            if ($LASTEXITCODE -ne 0) { throw "powercfg no pudo cambiar el tiempo de $($t.Text)." }
        }
        if ($hib) {
            & powercfg.exe /hibernate $hib | Out-Null
            if ($LASTEXITCODE -ne 0) { throw 'powercfg no pudo cambiar la hibernación (¿el equipo la admite?).' }
        }
        Write-Done
        return
    }

    $problems = @()
    if ($plan) {
        $active = & powercfg.exe /getactivescheme | Out-String
        if ($active -notmatch $plans[$plan]) { $problems += "el plan activo no es «$($planNames[$plan])»" }
    }
    foreach ($t in $timeouts) {
        $v = Get-Param $t.Param
        if ($null -eq $v) { continue }
        $current = Get-PowerMinutes $t.Sub $t.Name $t.Index
        if ($current -ne [int]$v) { $problems += "$($t.Text): $current min (debe ser $v)" }
    }
    if ($hib) {
        $on = (Get-RegistryValue 'HKLM:\SYSTEM\CurrentControlSet\Control\Power' 'HibernateEnabled') -eq 1
        if ($on -ne ($hib -eq 'on')) { $problems += "la hibernación está $(if ($on) { 'activada' } else { 'desactivada' })" }
    }
    if ($problems.Count -eq 0) {
        Write-Result $true 'La configuración de energía es la indicada.'
    }
    else {
        Write-Result $false ('Distinto de lo indicado: ' + ($problems -join '; ') + '.')
    }
