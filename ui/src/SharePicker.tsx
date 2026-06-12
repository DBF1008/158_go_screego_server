import React from 'react';
import {Box, Button, Checkbox, Divider, ListItemText, Menu, MenuItem} from '@mui/material';
import {RoomUser} from './message';

interface SharePickerProps {
    users: RoomUser[];
    anchorEl: HTMLElement | null;
    onClose: () => void;
    onShare: (targets?: string[]) => void;
}

export const SharePicker = ({users, anchorEl, onClose, onShare}: SharePickerProps) => {
    const others = users.filter((u) => !u.you);
    const [selected, setSelected] = React.useState<string[]>([]);

    const handleClose = () => {
        setSelected([]);
        onClose();
    };

    const toggle = (id: string) => {
        setSelected((current) =>
            current.includes(id) ? current.filter((x) => x !== id) : [...current, id]
        );
    };

    const shareEveryone = () => {
        onShare();
        handleClose();
    };

    const shareSelected = () => {
        onShare(selected);
        handleClose();
    };

    return (
        <Menu anchorEl={anchorEl} open={Boolean(anchorEl)} onClose={handleClose}>
            <MenuItem onClick={shareEveryone}>Everyone</MenuItem>
            {others.length > 0 && <Divider />}
            {others.map((user) => (
                <MenuItem key={user.id} onClick={() => toggle(user.id)}>
                    <Checkbox edge="start" checked={selected.includes(user.id)} disableRipple />
                    <ListItemText primary={user.name} />
                </MenuItem>
            ))}
            {others.length > 0 && (
                <Box sx={{px: 2, pt: 1}}>
                    <Button
                        variant="contained"
                        fullWidth
                        disabled={selected.length === 0}
                        onClick={shareSelected}
                    >
                        Present to selected ({selected.length})
                    </Button>
                </Box>
            )}
        </Menu>
    );
};
