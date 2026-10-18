class FocusClock {
  constructor(now) { this.lastUser = now; }
  activity(now) { this.lastUser = now; }
  expired(now) { return now - this.lastUser >= 180000; }
}
