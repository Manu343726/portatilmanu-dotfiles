#!/usr/bin/env python3
import dbus
import dbus.service
import dbus.mainloop.glib
from gi.repository import GLib

dbus.mainloop.glib.DBusGMainLoop(set_as_default=True)

class KdeWatcher(dbus.service.Object):
    def __init__(self, bus, path):
        super().__init__(bus, path)

    @dbus.service.method(
        "org.kde.StatusNotifierWatcher",
        in_signature="s",
        out_signature="",
    )
    def RegisterStatusNotifierItem(self, service):
        print(f"RegisterStatusNotifierItem: {service}")

    @dbus.service.method(
        "org.kde.StatusNotifierWatcher",
        in_signature="",
        out_signature="as",
    )
    def RegisteredStatusNotifierItems(self):
        return dbus.Array([], signature="s")

    @dbus.service.method(
        "org.kde.StatusNotifierWatcher",
        in_signature="",
        out_signature="i",
    )
    def ProtocolVersion(self):
        return 0

    @dbus.service.signal("org.kde.StatusNotifierWatcher", signature="s")
    def StatusNotifierItemRegistered(self, service):
        pass

    @dbus.service.signal("org.kde.StatusNotifierWatcher", signature="s")
    def StatusNotifierItemUnregistered(self, service):
        pass

bus = dbus.SessionBus()
name = dbus.service.BusName("org.kde.StatusNotifierWatcher", bus)
obj = KdeWatcher(bus, "/StatusNotifierWatcher")

loop = GLib.MainLoop()
print("StatusNotifierWatcher (KDE compat) running")
loop.run()
