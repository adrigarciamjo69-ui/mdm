# Descarga e instala las actualizaciones de Windows indicadas en MDM_IDS
# (identificadores separados por comas) y escribe una línea JSON con el
# resultado de cada una.
$ErrorActionPreference = 'Stop'
try {
    $wanted = @($env:MDM_IDS -split ',' | Where-Object { $_ })
    $session = New-Object -ComObject Microsoft.Update.Session
    $found = $session.CreateUpdateSearcher().Search("IsInstalled=0 and IsHidden=0 and Type='Software'").Updates
    $todo = New-Object -ComObject Microsoft.Update.UpdateColl
    foreach ($u in $found) {
        if ($wanted -contains [string]$u.Identity.UpdateID) {
            if (-not $u.EulaAccepted) { $u.AcceptEula() }
            [void]$todo.Add($u)
        }
    }
    $results = @()
    $reboot = $false
    if ($todo.Count -gt 0) {
        $downloader = $session.CreateUpdateDownloader()
        $downloader.Updates = $todo
        [void]$downloader.Download()
        $installer = $session.CreateUpdateInstaller()
        $installer.Updates = $todo
        $res = $installer.Install()
        $reboot = [bool]$res.RebootRequired
        for ($i = 0; $i -lt $todo.Count; $i++) {
            $r = $res.GetUpdateResult($i)
            # 2 = correcta, 3 = correcta con errores
            $ok = ($r.ResultCode -eq 2 -or $r.ResultCode -eq 3)
            $err = ''
            if (-not $ok) { $err = ('0x{0:X8}' -f $r.HResult) }
            $results += [pscustomobject]@{ id = [string]$todo.Item($i).Identity.UpdateID; status = $(if ($ok) { 'installed' } else { 'failed' }); error = $err }
        }
    }
    foreach ($id in $wanted) {
        if (-not ($results | Where-Object { $_.id -eq $id })) {
            $results += [pscustomobject]@{ id = $id; status = 'failed'; error = 'Ya no está disponible en Windows Update.' }
        }
    }
    [pscustomobject]@{ results = $results; reboot_pending = $reboot } | ConvertTo-Json -Compress -Depth 4
}
catch {
    [pscustomobject]@{ error = $_.Exception.Message } | ConvertTo-Json -Compress
    exit 1
}
