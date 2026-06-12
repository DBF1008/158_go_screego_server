const keyPrefix = 'screego_pw_';

export const storeRoomPassword = (roomId: string, password: string): void => {
    try {
        sessionStorage.setItem(keyPrefix + roomId, password);
    } catch {
        // sessionStorage may be unavailable
    }
};

export const getRoomPassword = (roomId: string): string | undefined => {
    try {
        return sessionStorage.getItem(keyPrefix + roomId) ?? undefined;
    } catch {
        return undefined;
    }
};

export const clearRoomPassword = (roomId: string): void => {
    try {
        sessionStorage.removeItem(keyPrefix + roomId);
    } catch {
        // sessionStorage may be unavailable
    }
};
