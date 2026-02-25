#!/bin/bash

# Asegurarnos de que estamos usando el axctl recién compilado
AXCTL="./axctl"

echo "=== Comprobación de estado inicial ==="
inhibited=$($AXCTL system is-inhibited)
echo "Estado de inhibición actual: $inhibited"

if [ "$inhibited" = "true" ]; then
    echo "¡Atención! El sistema está actualmente INHIBIDO (no entrará en idle real)."
    echo "Desactivando inhibición para la prueba..."
    $AXCTL system idle-inhibit 0
    echo "Nuevo estado: $($AXCTL system is-inhibited)"
fi

echo -e "\n=== Prueba 1: Esperando 5 segundos de inactividad ==="
echo "Por favor, NO toques el ratón ni el teclado..."

# Iniciamos el proceso que espera el idle en segundo plano para no bloquear si no ocurre
$AXCTL system idle-wait 5000 &
WAIT_PID=$!

# Bucle de comprobación cada segundo mientras esperamos
SECONDS_WAITED=0
while kill -0 $WAIT_PID 2>/dev/null; do
    sleep 1
    SECONDS_WAITED=$((SECONDS_WAITED + 1))
    
    # Comprobamos en tiempo real si nos han activado la inhibición desde otra terminal
    if [ "$($AXCTL system is-inhibited)" = "true" ]; then
        echo "[!] ALERTA: ¡Alguien activó la inhibición durante la espera!"
    fi
    
    echo "Esperando... ($SECONDS_WAITED seg)"
    
    if [ $SECONDS_WAITED -ge 10 ]; then
        echo "Han pasado 10 segundos y no se ha detectado el idle."
        echo "Probablemente sigues moviendo el ratón o un programa externo lo está inhibiendo."
        kill $WAIT_PID 2>/dev/null
        exit 1
    fi
done

echo "✅ ¡Inactividad de 5 segundos DETECTADA correctamente!"

echo -e "\n=== Prueba 2: Comprobando el estado de inactividad AHORA mismo ==="
is_idle=$($AXCTL system is-idle 5000)
echo "El comando is-idle 5000 devuelve: $is_idle"

echo -e "\n=== Prueba 3: Detectando que regresas ==="
echo "¡Mueve el ratón o toca una tecla ahora!"
$AXCTL system resume-wait 5000
echo "✅ ¡Actividad detectada! Has vuelto."

echo -e "\n=== Prueba 4: Comprobación de inhibición en runtime ==="
echo "Activando el modo cafeína (idle-inhibit 1)..."
$AXCTL system idle-inhibit 1
echo "Estado actual (is-inhibited): $($AXCTL system is-inhibited)"

echo "Intentando detectar 5s de inactividad estando inhibidos..."
echo "(No toques nada por 6 segundos... si el compositor ignora el inhibit para el notify, esto igual saltará, pero la pantalla no se apagará)"

$AXCTL system idle-wait 5000 &
WAIT_PID2=$!

sleep 6
if kill -0 $WAIT_PID2 2>/dev/null; do
    echo "La espera sigue bloqueada. ¡El compositor respetó la inhibición también para las notificaciones!"
    kill $WAIT_PID2 2>/dev/null
else
    echo "Se detectó la inactividad. Nota: Wayland a veces notifica inactividad de usuario aunque el apagado de pantalla esté inhibido."
fi

echo "Limpiando inhibidor..."
$AXCTL system idle-inhibit 0
echo "Estado final: $($AXCTL system is-inhibited)"
echo "¡Script de pruebas completado con éxito!"
