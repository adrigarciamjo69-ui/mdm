# Simulador de carga

Simula muchos agentes contra un servidor real para comprobar que aguanta el parque previsto. Cada agente se inscribe, hace check-in cada minuto (±10 %) y envía un inventario de 150 programas cuando el servidor lo pide. Al terminar muestra la latencia del check-in y da la prueba por **apta** si no ha habido ningún error y el percentil 99 no supera los 200 ms.

## Uso

1. En la consola, crea un token de inscripción llamado «Prueba de carga», con tantos usos como agentes vayas a simular y caducidad de un día.
2. Lanza la prueba desde otra máquina (para no medir el propio simulador en el servidor):

   ```sh
   go run ./scripts/loadtest --server https://mdm.empresa.es --token <token> --agents 1000 --duration 10m
   ```

   Si el servidor usa un certificado que la máquina de pruebas no reconoce, añade `--insecure`.

3. Las inscripciones se escalonan a 20 por segundo, así que 1.000 agentes tardan unos 50 segundos en estar todos dentro; la medición dura `--duration` a partir de ahí.

Cada 15 segundos se muestra el progreso, y al final un resumen como este:

```
Agentes inscritos:  1000 de 1000
Check-ins:          10512
Inventarios:        1000
Errores:            0
Latencia check-in:  p50 3.1ms, p95 8.4ms, p99 21.7ms (límite p99 200ms)

Resultado: apto
```

El programa termina con código 0 si es apto y 1 si no.

## Después de la prueba

Los equipos simulados quedan en la consola con nombres `carga-0000`, `carga-0001`… Para quitarlos, revoca el token y bórralos desde PostgreSQL:

```sh
docker compose exec -T db psql -U mdm -d mdm -c "DELETE FROM devices WHERE hostname LIKE 'carga-%' AND agent_version = 'loadtest';"
```
