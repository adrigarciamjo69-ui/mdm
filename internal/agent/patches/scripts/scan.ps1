# Busca actualizaciones pendientes con la API de Windows Update y escribe una
# línea JSON. La clasificación se deduce del identificador de la categoría,
# que no depende del idioma de Windows.
$ErrorActionPreference = 'Stop'
$classes = @{
    '0fa1201d-4330-4fa8-8ae9-b877473b6441' = 'security'
    'e6cf1350-c01b-414d-a61f-263d14d133b4' = 'critical'
    'e0789628-ce08-4437-be74-2495b842f43b' = 'definition'
    'b54e7d24-7add-428f-8b75-90a396fa584f' = 'feature_pack'
    'cd5ffd1e-e932-4e3a-bf74-18bf0b1bbd83' = 'update'
    '28bc880e-0592-4cbf-8f95-c79b17911d5f' = 'update'
    '68c5b0a3-d1a6-4553-ae49-01d3a7827828' = 'update'
    'b4832bd8-e735-4761-8daf-37f882276dab' = 'update'
    'ebfc1fc5-71a4-4f7b-9aca-3b9a503104a0' = 'driver'
}
function Test-RebootPending {
    (Test-Path 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\WindowsUpdate\Auto Update\RebootRequired') -or
    (Test-Path 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Component Based Servicing\RebootPending')
}
try {
    $session = New-Object -ComObject Microsoft.Update.Session
    $result = $session.CreateUpdateSearcher().Search("IsInstalled=0 and IsHidden=0 and Type='Software'")
    $updates = @()
    foreach ($u in $result.Updates) {
        $class = 'other'
        foreach ($c in $u.Categories) {
            $id = [string]$c.CategoryID
            if ($classes.ContainsKey($id)) { $class = $classes[$id]; break }
        }
        $kb = ''
        if ($u.KBArticleIDs.Count -gt 0) { $kb = 'KB' + $u.KBArticleIDs.Item(0) }
        $published = $null
        if ($u.LastDeploymentChangeTime) { $published = ([datetime]$u.LastDeploymentChangeTime).ToUniversalTime().ToString('o') }
        $updates += [pscustomobject]@{
            id = [string]$u.Identity.UpdateID; kb = $kb; title = [string]$u.Title; classification = $class
            severity = [string]$u.MsrcSeverity; size_bytes = [int64]$u.MaxDownloadSize; published_at = $published
            security = ($class -eq 'security' -or $class -eq 'critical')
        }
    }
    [pscustomobject]@{ updates = $updates; reboot_pending = [bool](Test-RebootPending) } | ConvertTo-Json -Compress -Depth 4
}
catch {
    [pscustomobject]@{ error = $_.Exception.Message } | ConvertTo-Json -Compress
    exit 1
}
