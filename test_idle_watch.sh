#!/bin/bash

AXCTL="./axctl"
TIMEOUT=5000

echo "=== Monitor de Actividad / Inactividad ==="
echo "Configurado para: $((TIMEOUT / 1000)) segundos"
echo "Presiona Ctrl+C para salir"
echo "------------------------------------------"

while true; do
    echo "👁️  Esperando inactividad de $((TIMEOUT / 1000))s... (Deja de tocar el teclado/ratón)"
    $AXCTL system input-idle-wait $TIMEOUT
    echo "💤 [$(date +%H:%M:%S)] ¡Inactividad detectada! Sistema en idle."
    
    echo "🖱️  Esperando actividad... (Mueve el ratón para continuar)"
    $AXCTL system input-resume-wait $TIMEOUT
    echo "⚡ [$(date +%H:%M:%S)] ¡Actividad detectada! Has vuelto."
    echo "------------------------------------------"
done
