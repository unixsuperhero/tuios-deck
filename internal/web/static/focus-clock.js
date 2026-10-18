class FocusClock {
  constructor(now) { this.lastUser = now; this.minuteStart = now; this.activeMinutes = 0; this.bufferUntil = 0; }
  activity(now) { this.lastUser = now; }
  expired(now) {
    if (now - this.lastUser >= 180000) return true;
    if (this.bufferUntil) return now >= this.bufferUntil;
    while (now >= this.minuteStart + 60000) {
      this.activeMinutes = this.lastUser >= this.minuteStart ? this.activeMinutes + 1 : 0;
      this.minuteStart += 60000;
      if (this.activeMinutes === 15) {
        this.bufferUntil = this.minuteStart + 30000;
        return now >= this.bufferUntil;
      }
    }
    return false;
  }
}
