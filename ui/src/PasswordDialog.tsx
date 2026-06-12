import React from 'react';
import {
    Button,
    Dialog,
    DialogActions,
    DialogContent,
    DialogTitle,
    TextField,
    Typography,
} from '@mui/material';

interface PasswordDialogProps {
    open: boolean;
    roomId: string;
    error?: string;
    onSubmit: (password: string) => void;
    onCancel: () => void;
}

export const PasswordDialog = ({open, roomId, error, onSubmit, onCancel}: PasswordDialogProps) => {
    const [password, setPassword] = React.useState('');

    React.useEffect(() => {
        if (open) {
            setPassword('');
        }
    }, [open]);

    const handleSubmit = (e: React.FormEvent) => {
        e.preventDefault();
        onSubmit(password);
    };

    return (
        <Dialog open={open} onClose={onCancel} maxWidth="xs" fullWidth>
            <form onSubmit={handleSubmit}>
                <DialogTitle>Room Password Required</DialogTitle>
                <DialogContent>
                    <Typography variant="body2" sx={{marginBottom: 1}}>
                        Room <strong>{roomId}</strong> requires a password to join.
                    </Typography>
                    <TextField
                        autoFocus
                        fullWidth
                        type="password"
                        label="Password"
                        value={password}
                        onChange={(e) => setPassword(e.target.value)}
                        error={!!error}
                        helperText={error}
                        margin="dense"
                    />
                </DialogContent>
                <DialogActions>
                    <Button onClick={onCancel}>Cancel</Button>
                    <Button type="submit" variant="contained" disabled={!password}>
                        Join
                    </Button>
                </DialogActions>
            </form>
        </Dialog>
    );
};
